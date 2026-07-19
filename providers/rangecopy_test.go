package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func init() {
	// 缩短段重试退避，让永久失败类测试不用等真实退避时间。
	segmentRetryBaseDelay = time.Millisecond
}

// memRangeStore 是内存里的 RangeRead/RangeWrite 双端实现，用于验证分段并行复制。
type memRangeStore struct {
	mu    sync.Mutex
	files map[string][]byte

	openReads  int
	openWrites int
	readErr    error
	// failReads 大于 0 时，前 failReads 次 OpenRangeReader 返回瞬时错误。
	failReads int
}

func newMemRangeStore() *memRangeStore {
	return &memRangeStore{files: map[string][]byte{}}
}

func (s *memRangeStore) put(name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = append([]byte(nil), data...)
}

func (s *memRangeStore) get(name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.files[name]...)
}

func (s *memRangeStore) OpenRangeReader(_ context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openReads++
	if s.readErr != nil {
		return nil, s.readErr
	}
	if s.failReads > 0 {
		s.failReads--
		return nil, fmt.Errorf("瞬时网络错误")
	}
	data, ok := s.files[name]
	if !ok {
		return nil, fmt.Errorf("文件不存在: %s", name)
	}
	if offset < 0 || offset+length > int64(len(data)) {
		return nil, fmt.Errorf("读取范围越界")
	}
	return io.NopCloser(bytes.NewReader(data[offset : offset+length])), nil
}

func (s *memRangeStore) Truncate(_ context.Context, name string, size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = make([]byte, size)
	return nil
}

func (s *memRangeStore) OpenRangeWriter(_ context.Context, name string, offset int64) (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openWrites++
	if _, ok := s.files[name]; !ok {
		return nil, fmt.Errorf("目标文件未预置: %s", name)
	}
	return &memRangeWriter{store: s, name: name, offset: offset}, nil
}

type memRangeWriter struct {
	store  *memRangeStore
	name   string
	offset int64
}

func (w *memRangeWriter) Write(p []byte) (int, error) {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	data := w.store.files[w.name]
	if w.offset+int64(len(p)) > int64(len(data)) {
		return 0, fmt.Errorf("写入越界")
	}
	copy(data[w.offset:], p)
	w.offset += int64(len(p))
	return len(p), nil
}

func (w *memRangeWriter) Close() error { return nil }

func randomBytes(n int) []byte {
	data := make([]byte, n)
	rng := rand.New(rand.NewSource(42))
	rng.Read(data)
	return data
}

func TestRangeCopyParallelSegments(t *testing.T) {
	source := newMemRangeStore()
	target := newMemRangeStore()
	data := randomBytes(4 << 20)
	source.put("src.bin", data)

	var lastProgress int64
	var mu sync.Mutex
	opts := RangeCopyOptions{MaxSegments: 4, MinSegmentSize: 1 << 20, BufferSize: 64 << 10, Progress: func(copied int64) {
		mu.Lock()
		lastProgress = copied
		mu.Unlock()
	}}
	if err := RangeCopy(context.Background(), source, "src.bin", int64(len(data)), target, "dst.bin", opts); err != nil {
		t.Fatalf("RangeCopy: %v", err)
	}
	if !bytes.Equal(target.get("dst.bin"), data) {
		t.Fatalf("目标内容与源不一致")
	}
	if source.openReads != 4 || target.openWrites != 4 {
		t.Fatalf("分段数 = %d/%d, want 4/4", source.openReads, target.openWrites)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastProgress != int64(len(data)) {
		t.Fatalf("最终进度 = %d, want %d", lastProgress, len(data))
	}
}

func TestRangeCopySmallFileSingleSegment(t *testing.T) {
	source := newMemRangeStore()
	target := newMemRangeStore()
	data := randomBytes(1000)
	source.put("src.bin", data)

	if err := RangeCopy(context.Background(), source, "src.bin", int64(len(data)), target, "dst.bin", RangeCopyOptions{}); err != nil {
		t.Fatalf("RangeCopy: %v", err)
	}
	if !bytes.Equal(target.get("dst.bin"), data) {
		t.Fatalf("目标内容与源不一致")
	}
	if source.openReads != 1 {
		t.Fatalf("小文件应单段复制, openReads = %d", source.openReads)
	}
}

func TestRangeCopyZeroSize(t *testing.T) {
	source := newMemRangeStore()
	target := newMemRangeStore()
	source.put("src.bin", nil)
	if err := RangeCopy(context.Background(), source, "src.bin", 0, target, "dst.bin", RangeCopyOptions{}); err != nil {
		t.Fatalf("RangeCopy: %v", err)
	}
	if got := target.get("dst.bin"); len(got) != 0 {
		t.Fatalf("零字节文件复制结果 = %d 字节", len(got))
	}
	if source.openReads != 0 {
		t.Fatalf("零字节文件不应打开读取器")
	}
}

func TestRangeCopyPropagatesSegmentError(t *testing.T) {
	source := newMemRangeStore()
	target := newMemRangeStore()
	source.put("src.bin", randomBytes(4<<20))
	wantErr := errors.New("网络断开")
	source.readErr = wantErr

	err := RangeCopy(context.Background(), source, "src.bin", 4<<20, target, "dst.bin", RangeCopyOptions{MaxSegments: 4, MinSegmentSize: 1 << 20})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrap %v", err, wantErr)
	}
}

func TestStreamCopyReportsProgress(t *testing.T) {
	data := randomBytes(256 << 10)
	var out bytes.Buffer
	var lastProgress int64
	var mu sync.Mutex
	n, err := StreamCopy(context.Background(), &out, bytes.NewReader(data), RangeCopyOptions{Progress: func(copied int64) {
		mu.Lock()
		lastProgress = copied
		mu.Unlock()
	}})
	if err != nil {
		t.Fatalf("StreamCopy: %v", err)
	}
	if n != int64(len(data)) || !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("复制结果不一致: n=%d", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastProgress != int64(len(data)) {
		t.Fatalf("最终进度 = %d, want %d", lastProgress, len(data))
	}
}

func TestRangeCopyRetriesTransientSegmentError(t *testing.T) {
	source := newMemRangeStore()
	target := newMemRangeStore()
	data := randomBytes(4 << 20)
	source.put("src.bin", data)
	source.failReads = 2

	var lastProgress int64
	var mu sync.Mutex
	opts := RangeCopyOptions{MaxSegments: 4, MinSegmentSize: 1 << 20, BufferSize: 64 << 10, Progress: func(copied int64) {
		mu.Lock()
		lastProgress = copied
		mu.Unlock()
	}}
	if err := RangeCopy(context.Background(), source, "src.bin", int64(len(data)), target, "dst.bin", opts); err != nil {
		t.Fatalf("瞬时错误应被段重试吸收: %v", err)
	}
	if !bytes.Equal(target.get("dst.bin"), data) {
		t.Fatalf("目标内容与源不一致")
	}
	if source.openReads != 6 {
		t.Fatalf("openReads = %d, want 6(4 段 + 2 次重试)", source.openReads)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastProgress != int64(len(data)) {
		t.Fatalf("重试后最终进度 = %d, want %d(重试不应重复计数)", lastProgress, len(data))
	}
}

func TestRangeCopyAdaptiveSegmentsCappedAt16(t *testing.T) {
	source := newMemRangeStore()
	target := newMemRangeStore()
	data := randomBytes(32 << 20)
	source.put("src.bin", data)

	// 32MiB / 1MiB 最小段 = 32 段,应封顶在默认上限 16。
	opts := RangeCopyOptions{MinSegmentSize: 1 << 20, BufferSize: 64 << 10}
	if err := RangeCopy(context.Background(), source, "src.bin", int64(len(data)), target, "dst.bin", opts); err != nil {
		t.Fatalf("RangeCopy: %v", err)
	}
	if !bytes.Equal(target.get("dst.bin"), data) {
		t.Fatalf("目标内容与源不一致")
	}
	if source.openReads != 16 || target.openWrites != 16 {
		t.Fatalf("分段数 = %d/%d, want 16/16", source.openReads, target.openWrites)
	}
}

func TestPipeCopyOddSizes(t *testing.T) {
	// 覆盖 空/不足一块/整块/整块+1/多块带尾巴 等边界,验证流水线切块的正确性。
	for _, size := range []int{0, 1, 4095, 4096, 4097, 40960, 41000} {
		data := randomBytes(size)
		var out bytes.Buffer
		n, err := pipeCopy(context.Background(), &out, bytes.NewReader(data), 4096, nil)
		if err != nil {
			t.Fatalf("size=%d: %v", size, err)
		}
		if n != int64(size) || !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("size=%d: 复制结果不一致, n=%d", size, n)
		}
	}
}

type failingWriter struct {
	writes int
	err    error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes >= 2 {
		return 0, w.err
	}
	return len(p), nil
}

func TestPipeCopyStopsOnWriteError(t *testing.T) {
	wantErr := errors.New("磁盘已满")
	w := &failingWriter{err: wantErr}
	n, err := pipeCopy(context.Background(), w, bytes.NewReader(randomBytes(1<<20)), 4096, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if n != 4096 {
		t.Fatalf("written = %d, want 4096", n)
	}
}

func TestStreamCopyHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := StreamCopy(ctx, io.Discard, bytes.NewReader(randomBytes(1<<20)), RangeCopyOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
