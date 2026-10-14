package project

import "time"

type Project struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Owner     *Member           `json:"owner,omitempty"`
	Members   []Member          `json:"members"`
	Labels    map[string]string `json:"labels"`
	Scores    [3]float64        `json:"scores"`
	Avatar    []byte            `json:"avatar"`
	CreatedAt time.Time         `json:"createdAt"`
	Parent    *Project          `json:"parent"`
	Internal  string            `json:"-"`
	secret    chan int
}

type Member struct {
	Name  string `json:"name"`
	Admin bool
}
