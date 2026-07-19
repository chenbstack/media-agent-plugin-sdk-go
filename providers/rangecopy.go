package providers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// RangeCopyOptions 控制 RangeCopy / StreamCopy 的分段与进度上报行为；零值使用默认参数。
// 分段数按文件大小自适应（size/MinSegmentSize，封顶 MaxSegments），调用方一般不需要设置。
type RangeCopyOptions struct {
	// MaxSegments 是并行分段数上限，默认 16；实际分段数按文件大小自适应。
	MaxSegments int
	// MinSegmentSize 是单段最小字节数；文件不足两段时退化为单段，默认 64MiB。
	MinSegmentSize int64
	// BufferSize 是每段复制缓冲大小，默认 1MiB；每段用两个缓冲做读写流水线。
	BufferSize int
	// Progress 是进度回调，见 ProgressFunc；nil 表示不上报。
	Progress ProgressFunc
}

const (
	defaultRangeCopyMaxSegments    = 16
	defaultRangeCopyMinSegmentSize = int64(64 << 20)
	defaultRangeCopyBufferSize     = 1 << 20
	progressReportInterval         = 500 * time.Millisecond
)

func (o RangeCopyOptions) withDefaults() RangeCopyOptions {
	if o.MaxSegments <= 0 {
		o.MaxSegments = defaultRangeCopyMaxSegments
	}
	if o.MinSegmentSize <= 0 {
		o.MinSegmentSize = defaultRangeCopyMinSegmentSize
	}
	if o.BufferSize <= 0 {
		o.BufferSize = defaultRangeCopyBufferSize
	}
	return o
}

// RangeCopy 把 source 的 sourcePath（共 size 字节）复制到 target 的 targetPath：
// 先 Truncate 预置目标为最终大小，再分段并行流式写入各自偏移；小文件退化为单段。
// 任一分段失败会取消其余分段并返回该错误，此时目标文件内容不完整。
func RangeCopy(ctx context.Context, source RangeReadProvider, sourcePath string, size int64, target RangeWriteProvider, targetPath string, opts RangeCopyOptions) error {
	if size < 0 {
		return fmt.Errorf("分段复制大小无效: %d", size)
	}
	opts = opts.withDefaults()
	if err := target.Truncate(ctx, targetPath, size); err != nil {
		return fmt.Errorf("预置目标文件大小: %w", err)
	}
	if size == 0 {
		reportProgressFinal(opts.Progress, 0)
		return nil
	}

	segments := int64(opts.MaxSegments)
	if bySize := size / opts.MinSegmentSize; bySize < segments {
		segments = bySize
	}
	if segments < 1 {
		segments = 1
	}
	segmentSize := (size + segments - 1) / segments

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var copied atomic.Int64
	stopProgress := startProgressReporter(&copied, opts.Progress)

	var wg sync.WaitGroup
	errCh := make(chan error, segments)
	for offset := int64(0); offset < size; offset += segmentSize {
		length := segmentSize
		if offset+length > size {
			length = size - offset
		}
		wg.Add(1)
		go func(offset, length int64) {
			defer wg.Done()
			if err := copySegment(ctx, source, sourcePath, target, targetPath, offset, length, opts.BufferSize, &copied); err != nil {
				errCh <- err
				cancel()
			}
		}(offset, length)
	}
	wg.Wait()
	stopProgress()
	select {
	case err := <-errCh:
		return err
	default:
	}
	reportProgressFinal(opts.Progress, size)
	return nil
}

func copySegment(ctx context.Context, source RangeReadProvider, sourcePath string, target RangeWriteProvider, targetPath string, offset, length int64, bufferSize int, copied *atomic.Int64) error {
	reader, err := source.OpenRangeReader(ctx, sourcePath, offset, length)
	if err != nil {
		return fmt.Errorf("打开源分段 [%d,%d): %w", offset, offset+length, err)
	}
	defer reader.Close()
	writer, err := target.OpenRangeWriter(ctx, targetPath, offset)
	if err != nil {
		return fmt.Errorf("打开目标分段 %d: %w", offset, err)
	}
	// LimitReader 防御实现多给数据。
	n, err := pipeCopy(ctx, writer, io.LimitReader(reader, length), bufferSize, copied)
	if err != nil {
		_ = writer.Close()
		return fmt.Errorf("复制分段 [%d,%d): %w", offset, offset+length, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("关闭目标分段 %d: %w", offset, err)
	}
	if n != length {
		return fmt.Errorf("分段 [%d,%d) 只复制了 %d 字节", offset, offset+length, n)
	}
	return nil
}

// StreamCopy 用 BufferSize 缓冲把 r 全量拷到 w，期间按 Progress 节流上报累计字节数。
// 返回写入的字节数；ctx 取消会让复制尽快失败。
func StreamCopy(ctx context.Context, w io.Writer, r io.Reader, opts RangeCopyOptions) (int64, error) {
	opts = opts.withDefaults()
	var copied atomic.Int64
	stop := startProgressReporter(&copied, opts.Progress)
	n, err := pipeCopy(ctx, w, r, opts.BufferSize, &copied)
	stop()
	if err == nil {
		reportProgressFinal(opts.Progress, n)
	}
	return n, err
}

// pipeCopy 用双缓冲把读和写重叠起来：读端预取下一块的同时写端在写上一块，
// 单条流的耗时从 读+写 之和变成两者取大。返回写入的字节数。
// 两个缓冲总量固定（2×bufferSize），读端最多领先写端一块，不会无界占内存。
func pipeCopy(ctx context.Context, dst io.Writer, src io.Reader, bufferSize int, copied *atomic.Int64) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type chunk struct {
		buf []byte
		n   int
	}
	free := make(chan []byte, 2)
	full := make(chan chunk, 2)
	free <- make([]byte, bufferSize)
	free <- make([]byte, bufferSize)
	readErr := make(chan error, 1)

	go func() {
		defer close(full)
		for {
			var buf []byte
			select {
			case <-ctx.Done():
				readErr <- ctx.Err()
				return
			case buf = <-free:
			}
			n, err := io.ReadFull(src, buf)
			if n > 0 {
				select {
				case <-ctx.Done():
					readErr <- ctx.Err()
					return
				case full <- chunk{buf: buf, n: n}:
				}
			}
			switch err {
			case nil:
			case io.EOF, io.ErrUnexpectedEOF:
				readErr <- nil
				return
			default:
				readErr <- err
				return
			}
		}
	}()

	var written int64
	for c := range full {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, err := dst.Write(c.buf[:c.n])
		if n > 0 {
			written += int64(n)
			if copied != nil {
				copied.Add(int64(n))
			}
		}
		if err != nil {
			return written, err
		}
		if n != c.n {
			return written, io.ErrShortWrite
		}
		free <- c.buf
	}
	return written, <-readErr
}

func startProgressReporter(copied *atomic.Int64, progress ProgressFunc) func() {
	if progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(progressReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				progress(copied.Load())
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func reportProgressFinal(progress ProgressFunc, size int64) {
	if progress != nil {
		progress(size)
	}
}
