package main

import (
	"OCRGO/internal/pkg/util"
	"OCRGO/internal/router"

	"github.com/labstack/echo/v4"
	// "CAGo/internal/router/swagger"
	presenterAi "OCRGO/internal/presenter/ai"
)

// @title           OCRGO API
// @version         1.0
// @description OCR API
// @contact.name 小蔡資訊
// @contact.url    https://jo87jimmy.github.io/
// @contact.email  jo87jimmy@gmail.com
// @host     localhost:9536
// @BasePath  /

func main() {
	// Initialize the application
	route := echo.New()

	presenterAi := presenterAi.NewImageToText()
	router := router.NewRouter(presenterAi)
	router.InitRoutes(route)

	// Start the application
	route.Logger.Fatal(route.Start(":" + util.Source["ENV"]["PORT"]))
}
