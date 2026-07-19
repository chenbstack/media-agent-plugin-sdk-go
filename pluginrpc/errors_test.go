package pluginrpc

import (
	"errors"
	"fmt"
	"net/rpc"
	"os"
	"testing"
)

func TestRPCErrorRoundTripPreservesSentinel(t *testing.T) {
	cases := []struct {
		name     string
		source   error
		sentinel error
	}{
		{"not_exist", fmt.Errorf("stat a/b.mkv: %w", os.ErrNotExist), os.ErrNotExist},
		{"permission", fmt.Errorf("open a/b.mkv: %w", os.ErrPermission), os.ErrPermission},
		{"exists", fmt.Errorf("mkdir a: %w", os.ErrExist), os.ErrExist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeRPCError(tc.source)
			// net/rpc 只传字符串,宿主拿到的是 rpc.ServerError。
			decoded := decodeRPCError(rpc.ServerError(encoded.Error()))
			if !errors.Is(decoded, tc.sentinel) {
				t.Fatalf("解码后应保留哨兵类型: %v", decoded)
			}
			if decoded.Error() != tc.source.Error() {
				t.Fatalf("解码后消息应去掉前缀: got %q want %q", decoded.Error(), tc.source.Error())
			}
		})
	}
}

func TestDecodeRPCErrorLegacyMessages(t *testing.T) {
	for _, msg := range []string{
		"stat 剧集\\雀骨 - S01E19.mkv: file does not exist",
		"stat /media/a.mkv: no such file or directory",
	} {
		decoded := decodeRPCError(rpc.ServerError(msg))
		if !errors.Is(decoded, os.ErrNotExist) {
			t.Fatalf("旧版插件文案应兜底识别为不存在: %q", msg)
		}
		if decoded.Error() != msg {
			t.Fatalf("兜底路径不应改写消息: %q", decoded.Error())
		}
	}
}

func TestDecodeRPCErrorPassthrough(t *testing.T) {
	plain := rpc.ServerError("连接超时")
	if decoded := decodeRPCError(plain); decoded != plain {
		t.Fatalf("无类别错误应原样返回: %v", decoded)
	}
	if decodeRPCError(nil) != nil {
		t.Fatal("nil 应保持 nil")
	}
	if encodeRPCError(nil) != nil {
		t.Fatal("encode nil 应保持 nil")
	}
}
