package api

import (
	"net/http"

	"example.com/clean/tifa"
)

type Server struct{}

func (s *Server) POST(w http.ResponseWriter, r *http.Request) {}

func helper(w http.ResponseWriter, r *http.Request) {}

func Load(c *tifa.Ctx) (string, error) { return "", nil }

func GET(c *tifa.Ctx) error {
	http.Redirect(c.Writer(), c.Request(), "/", http.StatusSeeOther)
	return nil
}
