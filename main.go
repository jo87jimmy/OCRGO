package main

import (
	"OCRGO/internal/pkg/util"
	"OCRGO/internal/router"

	_ "OCRGO/docs"
	presenterAi "OCRGO/internal/presenter/ai"

	"github.com/labstack/echo/v4"
)

// @title           OCRGO API
// @version         1.0
// @description OCR API
// @contact.name 小蔡資訊
// @contact.url    https://jo87jimmy.github.io/
// @contact.email  jo87jimmy@gmail.com
// @host     localhost:9541
// @BasePath  /

//http://127.0.0.1:9541/api/swagger/

func main() {
	// Initialize the application
	route := echo.New()

	presenterText := presenterAi.NewImageToText()
	presenterClass := presenterAi.NewImageClassification()
	router := router.NewRouter(presenterText, presenterClass)
	router.InitRoutes(route)

	// Start the application
	route.Logger.Fatal(route.Start(":" + util.Source["ENV"]["PORT"]))
}
