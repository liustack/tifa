package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v2"
	"github.com/labstack/echo/v4"
)

func GET(c *gin.Context) {}

func POST(c echo.Context) error { return nil }

func PUT(c *fiber.Ctx) error { return nil }

func PATCH(w http.ResponseWriter, r *http.Request) {}

func OPTIONS(w http.ResponseWriter, r *http.Request) {}
