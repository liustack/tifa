// Package rss is the RSS feed of the posts of a language. It reads the collection on every
// request, so a post whose date comes shows up without a new build.
package rss

import (
	"encoding/xml"
	"slices"
	"time"

	"github.com/liustack/tifa"

	"example.com/contenttest"
	"example.com/contenttest/app/lang+/blog"
)

const site = "https://example.com"

func GET(c *tifa.Ctx) error {
	l := c.Param("lang")
	if !slices.Contains(contenttest.Blog.Languages(), l) {
		return tifa.ErrNotFound
	}
	type item struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
	}
	type channel struct {
		Title    string `xml:"title"`
		Link     string `xml:"link"`
		Language string `xml:"language"`
		Items    []item `xml:"item"`
	}
	ch := channel{Title: "contenttest", Link: site + "/" + l, Language: l}
	for _, p := range blog.Published(l) {
		u := site + "/" + l + "/blog/" + p.Slug
		ch.Items = append(ch.Items, item{Title: p.Title, Link: u, GUID: u, Description: p.Description, PubDate: p.Date.Format(time.RFC1123Z)})
	}
	out, err := xml.MarshalIndent(struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel channel  `xml:"channel"`
	}{Version: "2.0", Channel: ch}, "", "  ")
	if err != nil {
		return err
	}
	w := c.Writer()
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, err = w.Write(append([]byte(xml.Header), out...))
	return err
}
