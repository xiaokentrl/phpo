// 应用升级模型：§5.6 update:* 事件载荷
package model

type UpdateAvailable struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
	Size      int64  `json:"size"`
}

type UpdateProgress struct {
	Stage   string  `json:"stage"` // download / verify / install
	Percent int     `json:"percent"`
	Speed   float64 `json:"speed"`
}

type UpdateDone struct {
	Status  TaskStatus `json:"status"`
	Version string     `json:"version"`
}
