// Package lang is the guide in each of its languages, /guide/en-US and /guide/zh-CN. The Layout
// gives the language of the page, and its head sets it on <html>.
package lang

import "github.com/liustack/tifa"

type Guide struct {
	Lang  string `json:"lang"`
	Title string `json:"title"`
	// The other language, for the link that switches to it.
	Other     string `json:"other"`
	OtherName string `json:"otherName"`
	NotFound  string `json:"notFound"`
}

var guides = map[string]Guide{
	"en-US": {Lang: "en-US", Title: "Guide", Other: "zh-CN", OtherName: "中文", NotFound: "No such topic."},
	"zh-CN": {Lang: "zh-CN", Title: "指南", Other: "en-US", OtherName: "English", NotFound: "没有这个主题。"},
}

func Layout(c *tifa.Ctx) (Guide, error) {
	guide, ok := guides[c.Param("lang")]
	if !ok {
		return Guide{}, tifa.ErrNotFound
	}
	return guide, nil
}
