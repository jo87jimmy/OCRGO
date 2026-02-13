package router

import (
	"net/http"

	"OCRGO/docs"
	"OCRGO/internal/pkg/util"
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
		// 使用 net/http 的常量，因為 echo v4 不再匯出 HTTP 方法常量
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
	}))
	//蔡- swaggerEcho 如果 host 設定為     ""localhost""":9516 下面這段必加 因為要轉其他的ip 才不會遇到寫不進去cookie
	if util.Source["ENV"]["SWAGGEROUTE"] != "" {
		docs.SwaggerInfo.Title = util.Source["ENV"]["SWAGGERTITLE"]
		docs.SwaggerInfo.Host = util.Source["ENV"]["SWAGGEROUTE"] + ":" + util.Source["ENV"]["PORT"]
		docs.SwaggerInfo.BasePath = "/"
	}

	// API Routes
	api := e.Group("/api")
	api.GET("/swagger/*any", echoSwagger.WrapHandler)

	ai := api.Group("/ai")
	ai.POST("/image/orc/text", r.imageToTextPresenter.ExtractText)
	ai.POST("/image/classification", r.imageToClassificationPresenter.ClassifyImage)
	ai.POST("/image/orc/text/v2", r.imageToTextPresenterV2.ExtractText)
	// ai.POST("/image/classification/v2", r.imageToClassificationPresenterV2.ClassifyImage)

}

type Router struct {
	imageToTextPresenter             ai.ImageToTextPresenter
	imageToClassificationPresenter   ai.ImageClassificationPresenter
	imageToTextPresenterV2           ai.ImageToTextPresenterV2
	imageToClassificationPresenterV2 ai.ImageClassificationPresenterV2
}

func NewRouter(aiText ai.ImageToTextPresenter, aiClass ai.ImageClassificationPresenter, aiTextV2 ai.ImageToTextPresenterV2, aiClassV2 ai.ImageClassificationPresenterV2) IRouter {
	// func NewRouter(aiText ai.ImageToTextPresenter, aiClass ai.ImageClassificationPresenter, aiTextV2 ai.ImageToTextPresenterV2) IRouter {
	return &Router{
		imageToTextPresenter:             aiText,
		imageToClassificationPresenter:   aiClass,
		imageToTextPresenterV2:           aiTextV2,
		imageToClassificationPresenterV2: aiClassV2,
	}
}
