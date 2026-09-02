package pluginrpc

import (
	"context"
	"testing"

	pluginsdk "github.com/chenbstack/media-agent-plugin-sdk-go"
)

type memoryDownloadTasks struct{}

func (memoryDownloadTasks) ListDownloaders(context.Context) ([]pluginsdk.DownloaderInfo, error) {
	return []pluginsdk.DownloaderInfo{{ID: "dl-1", Name: "qb", Kind: "qbittorrent", Enabled: true, Default: true}}, nil
}

func (memoryDownloadTasks) ListTasks(context.Context, pluginsdk.DownloadTaskQuery) ([]pluginsdk.DownloadTaskInfo, error) {
	return []pluginsdk.DownloadTaskInfo{{ID: "task-1", Origin: "host"}}, nil
}

func (memoryDownloadTasks) GetTask(context.Context, string) (pluginsdk.DownloadTaskInfo, error) {
	return pluginsdk.DownloadTaskInfo{ID: "task-1"}, nil
}

func (memoryDownloadTasks) ListTaskFiles(context.Context, string) ([]pluginsdk.DownloadFileInfo, error) {
	return []pluginsdk.DownloadFileInfo{{Index: 0, Path: "a.mkv"}}, nil
}

type memoryDownloadControl struct{ removed []string }

func (c *memoryDownloadControl) AddTorrent(context.Context, pluginsdk.AddTorrentInput) (pluginsdk.DownloadTaskInfo, error) {
	return pluginsdk.DownloadTaskInfo{ID: "task-new"}, nil
}
func (c *memoryDownloadControl) PauseTask(context.Context, string) error  { return nil }
func (c *memoryDownloadControl) ResumeTask(context.Context, string) error { return nil }
func (c *memoryDownloadControl) RemoveTask(_ context.Context, taskID string, _ bool) error {
	c.removed = append(c.removed, taskID)
	return nil
}
func (c *memoryDownloadControl) SetFileSelection(context.Context, string, []pluginsdk.DownloadFileSelection) error {
	return nil
}

type memoryTorrentPool struct{}

func (memoryTorrentPool) ListPoolTorrents(context.Context, pluginsdk.PoolTorrentQuery) ([]pluginsdk.PoolTorrent, error) {
	return []pluginsdk.PoolTorrent{{ID: "pool-1", Release: pluginsdk.PoolRelease{State: pluginsdk.PoolReleased}}}, nil
}

func newDownloadControlServer(control *memoryDownloadControl) hostServicesServer {
	return *newHostServicesServer(&hostServicesState{
		ctx:             context.Background(),
		downloadTasks:   memoryDownloadTasks{},
		downloadControl: control,
		torrentPool:     memoryTorrentPool{},
	})
}

// 读与写是两条权限，光有读权限不能动下载器。这是这组能力的安全边界，单独立一个
// 测试盯住：把 downloads.control 漏加进某个读处理器，光看代码不容易发现。
func TestDownloadTasksReadPermissionGrantsNoControl(t *testing.T) {
	control := &memoryDownloadControl{}
	server := newDownloadControlServer(control)
	server.live().permissions.Host = []string{"downloads.tasks.read"}

	var reply JSONReply
	if err := server.ListDownloaders(Empty{}, &reply); err != nil {
		t.Fatalf("ListDownloaders with read permission: %v", err)
	}
	if err := server.ListDownloadTasks(DownloadTaskQueryRequest{}, &reply); err != nil {
		t.Fatalf("ListDownloadTasks with read permission: %v", err)
	}
	if err := server.GetDownloadTask(DownloadTaskIDRequest{TaskID: "task-1"}, &reply); err != nil {
		t.Fatalf("GetDownloadTask with read permission: %v", err)
	}
	if err := server.ListDownloadTaskFiles(DownloadTaskIDRequest{TaskID: "task-1"}, &reply); err != nil {
		t.Fatalf("ListDownloadTaskFiles with read permission: %v", err)
	}

	for name, call := range map[string]func() error{
		"AddTorrent":               func() error { return server.AddTorrent(AddTorrentRequest{}, &reply) },
		"PauseDownloadTask":        func() error { return server.PauseDownloadTask(DownloadTaskIDRequest{}, &reply) },
		"ResumeDownloadTask":       func() error { return server.ResumeDownloadTask(DownloadTaskIDRequest{}, &reply) },
		"RemoveDownloadTask":       func() error { return server.RemoveDownloadTask(DownloadTaskRemoveRequest{}, &reply) },
		"SetDownloadFileSelection": func() error { return server.SetDownloadFileSelection(DownloadFileSelectionRequest{}, &reply) },
	} {
		if err := call(); err == nil {
			t.Fatalf("%s 只有 downloads.tasks.read 时应当被拒绝", name)
		}
	}
	if len(control.removed) != 0 {
		t.Fatalf("被拒绝的调用不应打到宿主能力: %v", control.removed)
	}
}

func TestDownloadControlPermissionAllowsWrites(t *testing.T) {
	control := &memoryDownloadControl{}
	server := newDownloadControlServer(control)
	server.live().permissions.Host = []string{"downloads.control"}

	var reply JSONReply
	if err := server.AddTorrent(AddTorrentRequest{}, &reply); err != nil {
		t.Fatalf("AddTorrent with control permission: %v", err)
	}
	if err := server.RemoveDownloadTask(DownloadTaskRemoveRequest{TaskID: "task-1", DeleteData: true}, &reply); err != nil {
		t.Fatalf("RemoveDownloadTask with control permission: %v", err)
	}
	if len(control.removed) != 1 || control.removed[0] != "task-1" {
		t.Fatalf("RemoveTask 未透传 taskID: %v", control.removed)
	}
	// 反过来同样要成立：控制权限不顺带给出只读权限。
	if err := server.ListDownloadTasks(DownloadTaskQueryRequest{}, &reply); err == nil {
		t.Fatal("只有 downloads.control 时不应能列出任务")
	}
}

func TestTorrentPoolPermissionIsSeparate(t *testing.T) {
	server := newDownloadControlServer(&memoryDownloadControl{})
	var reply JSONReply
	if err := server.ListPoolTorrents(PoolTorrentQueryRequest{}, &reply); err == nil {
		t.Fatal("没有 site.torrents.pool.read 时不应能读种子池")
	}
	server.live().permissions.Host = []string{"downloads.tasks.read", "downloads.control"}
	if err := server.ListPoolTorrents(PoolTorrentQueryRequest{}, &reply); err == nil {
		t.Fatal("下载相关权限不应顺带给出种子池读取权限")
	}
	server.live().permissions.Host = []string{"site.torrents.pool.read"}
	if err := server.ListPoolTorrents(PoolTorrentQueryRequest{}, &reply); err != nil {
		t.Fatalf("ListPoolTorrents with permission: %v", err)
	}
}

// 宿主没注入能力时给的是「未提供」而不是「没授权」，排查时两种原因不能混。
func TestDownloadCapabilitiesReportMissingHostServices(t *testing.T) {
	server := *newHostServicesServer(&hostServicesState{ctx: context.Background()})
	server.live().permissions.Host = []string{"downloads.tasks.read", "downloads.control", "site.torrents.pool.read"}
	var reply JSONReply
	for name, call := range map[string]func() error{
		"ListDownloadTasks": func() error { return server.ListDownloadTasks(DownloadTaskQueryRequest{}, &reply) },
		"AddTorrent":        func() error { return server.AddTorrent(AddTorrentRequest{}, &reply) },
		"ListPoolTorrents":  func() error { return server.ListPoolTorrents(PoolTorrentQueryRequest{}, &reply) },
	} {
		if err := call(); err == nil {
			t.Fatalf("%s 在宿主未注入能力时应当失败", name)
		}
	}
}
