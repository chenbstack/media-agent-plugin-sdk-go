package pluginsdk

import "context"

// 本文件是插件向宿主索取媒体元数据的契约。
//
// 反方向的那条链（providers.MetadataProvider）是「宿主让数据源插件去抓元数据」，
// 请求里带着数据源自己的 ID 空间和凭据。这里是消费方：插件报一个宿主的 mediaID，
// 宿主自己去查是哪个数据源、哪条记录，自己带凭据去取。两者不能共用类型——消费方
// 插件既拿不到也不该拿到数据源的 API key，更不该按「这次是 TMDB 还是豆瓣」写分支。
//
// 边界和 TextGeneration 是同一条：宿主替插件持有秘密与选型，只交出办事所需的结果。

// MediaMetadataCastMember 是一位演职人员。
//
// 只有名字和角色名：这是给插件当上下文用的，不是拿来渲染演员表的，头像之类的
// 展示字段不往下发。
type MediaMetadataCastMember struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
}

// MediaMetadataDetail 是一部媒体的元数据，字段裁到消费方插件办事所需。
//
// 数据源原始响应（providers.MetaDetail.Raw）、数据源标识、季集结构都不在这里：
// 前者可能夹带上游接口的内部字段，后两者会诱使插件按数据源写分支——宿主换个源，
// 插件就断了。
type MediaMetadataDetail struct {
	MediaType     string `json:"media_type"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
	Overview      string `json:"overview,omitempty"`
	// Language 是宿主**实际按哪种语言**去取的，不是请求参数的回显。
	//
	// 为空表示按数据源自己配置的语言取的——要么调用方没指定，要么这个数据源不支持
	// 指定语言。插件必须看它来判断第二侧到底拿到没有：把一份中文角色名当成原文喂给
	// 模型，比没有角色表更糟，模型会拿它去对应台词里根本不存在的词。
	//
	// 它保证不了字段级的语言。数据源缺某门语言的译文时通常回落到原文，所以即使这里
	// 是 zh-CN，个别字段仍可能是原文——这是数据源的事实，宿主不替它编。
	Language string                    `json:"language,omitempty"`
	Cast     []MediaMetadataCastMember `json:"cast,omitempty"`
}

// MediaMetadata 让插件按语言读取宿主媒体库里某部媒体的元数据。
//
// 需要 host 权限 "media.metadata.read"。
type MediaMetadata interface {
	// Detail 取一部媒体的元数据。mediaID 是宿主的媒体 id（钩子 payload 里的
	// media.id）；language 是 BCP 47 语言标签，留空表示用宿主数据源自己配置的语言。
	//
	// 指定 language 的用途是拿同一部片的第二种语言。库里存的永远是用户配置语言的
	// 那一份，而有些活儿要两侧凑对照才成立：字幕翻译里，台词出现的是原文角色名，
	// 元数据存的是译名，只给模型一侧它对应不上——实测给一份纯中文角色名清单，
	// 模型照样把 Scales 译成「鳞片」而不是清单里的「鳞鳞」。
	//
	// 数据源不支持指定语言时不报错，返回它自己那一份并在 Language 里说明。
	Detail(ctx context.Context, mediaID, language string) (MediaMetadataDetail, error)
}
