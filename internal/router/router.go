package router

import (
	"OCRGO/internal/presenter/ai"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"
)

type IRouter interface {
	InitRoutes(*echo.Echo)
}

func (r *Router) InitRoutes(e *echo.Echo) {
	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.POST, echo.PUT, echo.DELETE, echo.OPTIONS},
	}))

	// API Routes
	api := e.Group("/api")
	api.GET("/swagger/*any", echoSwagger.WrapHandler)

	// Add more routes here
	ai := api.Group("/ai")
	ai.POST("/imageToText", r.imageToTextPresenter.PaddXServi)

}

type Router struct {
	imageToTextPresenter ai.IImageToTextPresenter
}

func NewRouter(ai ai.IImageToTextPresenter) IRouter {
	return &Router{
		imageToTextPresenter: ai,
	}
}
