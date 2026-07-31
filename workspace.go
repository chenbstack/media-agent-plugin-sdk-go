package pluginsdk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace 是宿主分配给单个插件的私有工作目录。
//
// 它和 MediaSidecars / MediaMirrors 是两类东西，别混用：
//
//   - Sidecars / Mirrors 写的是**用户的**文件（媒体库里的字幕、另一个存储里的 .strm）。
//     那里插件连路径都拿不到，宿主按 FileRef 推导，每个字节都过一趟 RPC——因为写错
//     地方的代价是弄脏用户的媒体库。
//   - Workspace 写的是**插件自己的**文件：解压出来的临时目录、下载缓存、编译产物。
//     用户不看也不改。这里给的是一个真实路径，插件自己用 os.* 操作——把几百 MB 的
//     解压过程拆成 RPC 调用既慢又没意义。
//
// 边界不在这个类型上，而在目录本身：一个插件一个，宿主创建时 chown 给沙箱身份，
// 别的插件的工作目录和用户的媒体目录都不在里面。下面几个方法会挡住 ../ 越界，
// 但那是防手滑，不是安全防线——插件进程本来就能 open 任意路径。
//
// 需要 host 权限 "workspace.local"；没声明的插件拿到的是 nil。
type Workspace struct {
	root string
}

// NewWorkspace 绑定一个已经存在的目录。root 为空返回 nil，
// 调用方因此可以直接把结果赋给 Instance.Workspace。
func NewWorkspace(root string) *Workspace {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	return &Workspace{root: filepath.Clean(root)}
}

// Root 是工作目录的绝对路径。需要把路径交给外部命令（如给 ffmpeg 传 -o）时用它。
func (w *Workspace) Root() string {
	if w == nil {
		return ""
	}
	return w.root
}

// Path 把工作目录内的相对路径解析成绝对路径，不创建任何东西。
func (w *Workspace) Path(rel string) (string, error) {
	if w == nil {
		return "", fmt.Errorf("插件未获得私有工作目录：未声明 host 权限 workspace.local，或宿主分配目录失败（见宿主日志）")
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return w.root, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("工作目录内的路径必须是相对路径: %s", rel)
	}
	full := filepath.Join(w.root, filepath.FromSlash(rel))
	// filepath.Join 已经 Clean 过，这里判的是 Clean 之后还在不在根下面。
	if full != w.root && !strings.HasPrefix(full, w.root+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越出工作目录: %s", rel)
	}
	return full, nil
}

// MkdirAll 在工作目录下建子目录，返回它的绝对路径。
func (w *Workspace) MkdirAll(rel string) (string, error) {
	full, err := w.Path(rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return "", err
	}
	return full, nil
}

// Create 建一个文件，父目录自动补齐。
func (w *Workspace) Create(rel string) (*os.File, error) {
	full, err := w.Path(rel)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	return os.Create(full)
}

// Open 只读打开工作目录里的文件。
func (w *Workspace) Open(rel string) (*os.File, error) {
	full, err := w.Path(rel)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

// WriteFile 覆盖写一个文件，父目录自动补齐。
func (w *Workspace) WriteFile(rel string, data []byte) error {
	full, err := w.Path(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

// ReadFile 读工作目录里的文件。
func (w *Workspace) ReadFile(rel string) ([]byte, error) {
	full, err := w.Path(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// RemoveAll 删掉工作目录里的一棵子树。rel 为空时清空工作目录本身但保留目录，
// 这样插件不必关心宿主什么时候重建它。
func (w *Workspace) RemoveAll(rel string) error {
	full, err := w.Path(rel)
	if err != nil {
		return err
	}
	if full == w.Root() {
		entries, err := os.ReadDir(full)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := os.RemoveAll(filepath.Join(full, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return os.RemoveAll(full)
}
