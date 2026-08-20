package pluginrpc

import (
	"errors"
	"os"
	"strings"
)

// net/rpc 跨进程只保留错误文本，os.ErrNotExist 等哨兵在宿主侧全部丢失，
// 宿主的 errors.Is 判断（如"目标文件不存在是正常情况"）会失效。
// encodeRPCError 在插件侧把错误类别编码进消息前缀，decodeRPCError 在宿主侧
// 还原成 wrap 对应哨兵的错误，使 errors.Is 跨 RPC 边界恢复工作。

const (
	rpcErrNotFoundPrefix   = "[maerr:not_found] "
	rpcErrPermissionPrefix = "[maerr:permission] "
	rpcErrExistsPrefix     = "[maerr:already_exists] "
	rpcErrConfigMissPrefix = "[maerr:config_miss] "
)

// ErrConfigNotCached 是插件报「宿主只发了配置摘要，但我这里没有对应的那份」。宿主会
// 丢掉自己的记录、带完整配置重试一次，插件方不需要处理它。
var ErrConfigNotCached = errors.New("插件未缓存该配置摘要")

func encodeRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return errors.New(rpcErrNotFoundPrefix + err.Error())
	case errors.Is(err, os.ErrPermission):
		return errors.New(rpcErrPermissionPrefix + err.Error())
	case errors.Is(err, os.ErrExist):
		return errors.New(rpcErrExistsPrefix + err.Error())
	}
	return err
}

type typedRPCError struct {
	msg      string
	sentinel error
}

func (e *typedRPCError) Error() string { return e.msg }
func (e *typedRPCError) Unwrap() error { return e.sentinel }

func decodeRPCError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, rpcErrNotFoundPrefix):
		return &typedRPCError{msg: strings.TrimPrefix(msg, rpcErrNotFoundPrefix), sentinel: os.ErrNotExist}
	case strings.HasPrefix(msg, rpcErrPermissionPrefix):
		return &typedRPCError{msg: strings.TrimPrefix(msg, rpcErrPermissionPrefix), sentinel: os.ErrPermission}
	case strings.HasPrefix(msg, rpcErrExistsPrefix):
		return &typedRPCError{msg: strings.TrimPrefix(msg, rpcErrExistsPrefix), sentinel: os.ErrExist}
	case strings.HasPrefix(msg, rpcErrConfigMissPrefix):
		return &typedRPCError{msg: strings.TrimPrefix(msg, rpcErrConfigMissPrefix), sentinel: ErrConfigNotCached}
	}
	// 旧版插件没有前缀：按本地文件系统、go-smb2 与 Windows 的既有文案兜底识别"不存在"。
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "file does not exist") ||
		strings.Contains(lower, "cannot find the file") ||
		strings.Contains(lower, "cannot find the path") {
		return &typedRPCError{msg: msg, sentinel: os.ErrNotExist}
	}
	return err
}
