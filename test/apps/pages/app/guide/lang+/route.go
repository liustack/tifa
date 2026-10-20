package lang

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) {
	guide, err := tifa.Use(c, Layout)
	if err != nil {
		return "", err
	}
	if guide.Lang == "zh-CN" {
		return "欢迎。", nil
	}
	return "Welcome.", nil
}
