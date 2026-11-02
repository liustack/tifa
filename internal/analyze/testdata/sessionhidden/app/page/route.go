package page

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return "x", nil }

func signIn[T any](c *tifa.Ctx) error {
	return tifa.SetSession(c, &struct {
		ID int
		_  T `json:"-"`
	}{})
}

func POST(c *tifa.Ctx) error {
	if err := signIn[int](c); err != nil {
		return err
	}
	return signIn[string](c)
}
