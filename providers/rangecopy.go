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
type RangeCopyOptions struct {
	// MaxSegments 是并行分段数上限，默认 4。
	MaxSegments int
	// MinSegmentSize 是单段最小字节数；文件不足两段时退化为单段，默认 64MiB。
	MinSegmentSize int64
	// BufferSize 是每段复制缓冲大小，默认 1MiB。
	BufferSize int
	// Progress 是进度回调，见 ProgressFunc；nil 表示不上报。
	Progress ProgressFunc
}

const (
	defaultRangeCopyMaxSegments    = 4
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
	buf := make([]byte, bufferSize)
	// LimitReader 防御实现多给数据，同时剥掉可能的 WriterTo 让缓冲生效。
	n, err := io.CopyBuffer(&countingContextWriter{ctx: ctx, w: writer, copied: copied}, io.LimitReader(reader, length), buf)
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
// 返回写入的字节数；ctx 取消会让下一次写入尽快失败。
func StreamCopy(ctx context.Context, w io.Writer, r io.Reader, opts RangeCopyOptions) (int64, error) {
	opts = opts.withDefaults()
	var copied atomic.Int64
	stop := startProgressReporter(&copied, opts.Progress)
	buf := make([]byte, opts.BufferSize)
	// struct 包装剥掉 r 可能实现的 WriterTo，保证 CopyBuffer 使用我们的缓冲。
	n, err := io.CopyBuffer(&countingContextWriter{ctx: ctx, w: w, copied: &copied}, struct{ io.Reader }{r}, buf)
	stop()
	if err == nil {
		reportProgressFinal(opts.Progress, n)
	}
	return n, err
}

type countingContextWriter struct {
	ctx    context.Context
	w      io.Writer
	copied *atomic.Int64
}

func (w *countingContextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.w.Write(p)
	if n > 0 && w.copied != nil {
		w.copied.Add(int64(n))
	}
	return n, err
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
