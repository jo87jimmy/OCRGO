package router // 定義套件名稱為 router，負責應用程式的 HTTP 路由配置與管理

import (
	"net/http" // 引入標準庫 net/http，用於處理 HTTP 協議相關常數與功能

	"OCRGO/docs"                  // 引入 docs 套件，用於 Swagger API 文件生成與設定
	"OCRGO/internal/pkg/util"     // 引入內部工具套件 util，用於讀取配置與環境變數等
	"OCRGO/internal/presenter/ai" // 引入 AI 展現層套件，包含 OCR 與影像分類的處理邏輯

	"github.com/labstack/echo/v4"                // 引入 Echo 網頁框架 v4 版本，用於建立高效能 Web 服務
	"github.com/labstack/echo/v4/middleware"     // 引入 Echo 中間件套件，提供日誌、恢復與 CORS 等功能
	echoSwagger "github.com/swaggo/echo-swagger" // 引入 Echo Swagger 套件，用於整合 Swagger UI 到 Echo 應用中
)

// IRouter 介面定義了路由初始化的合約，確保任何實作此介面的結構體都必須包含 InitRoutes 方法
type IRouter interface {
	InitRoutes(*echo.Echo) // InitRoutes 方法接收一個 Echo 實例，用於註冊應用程式的路由
}

// InitRoutes 方法為 Router 結構體實作 IRouter 介面，負責設定中間件與定義 API 路由
func (r *Router) InitRoutes(e *echo.Echo) {
	// Middleware 中間件設定區塊
	e.Use(middleware.Logger())                             // 啟用 Logger 中間件，記錄每個 HTTP 請求的詳細資訊，便於除錯與監控
	e.Use(middleware.Recover())                            // 啟用 Recover 中間件，當處理請求發生 panic 時自動恢復，防止伺服器崩潰
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{ // 設定 CORS (跨來源資源共用) 配置，允許不同來源的前端存取 API
		AllowOrigins: []string{"*"}, // 允許所有來源 (*) 進行跨域請求，開發階段方便測試，生產環境建議限制特定網域
		// 使用 net/http 的常量，因為 echo v4 不再匯出 HTTP 方法常量
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}, // 明確列出允許的 HTTP 方法
	}))

	// Swagger 配置區塊
	// 蔡- swaggerEcho 如果 host 設定為 ""localhost"":9516 下面這段必加 因為要轉其他的ip 才不會遇到寫不進去cookie
	// 檢查環境變數 SWAGGEROUTE 是否存在，若存在則動態設定 Swagger 資訊
	if util.Source["ENV"]["SWAGGEROUTE"] != "" {
		docs.SwaggerInfo.Title = util.Source["ENV"]["SWAGGERTITLE"]                                  // 設定 Swagger 文件標題，從環境變數讀取
		docs.SwaggerInfo.Host = util.Source["ENV"]["SWAGGEROUTE"] + ":" + util.Source["ENV"]["PORT"] // 設定 Swagger Host 地址，組合主機與埠號
		docs.SwaggerInfo.BasePath = "/"                                                              // 設定 API 基本路徑為根目錄
	}

	// API Routes 路由定義區塊
	api := e.Group("/api")                            // 建立一個路由群組 "/api"，所有此群組下的路徑都會以此開頭
	api.GET("/swagger/*any", echoSwagger.WrapHandler) // 註冊 Swagger UI 路由，訪問 /api/swagger/* 即可查看 API 文件

	ai := api.Group("/ai")                                                                // 在 "/api" 下建立子路由群組 "/ai"，專門處理 AI 相關請求
	ai.POST("/image/orc/text", r.imageToTextPresenter.ExtractText)                        // 註冊 POST /api/ai/image/orc/text路由，處理圖片 OCR 轉文字請求
	ai.POST("/image/classification", r.imageToClassificationPresenter.ClassifyImage)      // 註冊 POST /api/ai/image/classification 路由，處理圖片分類請求
	ai.POST("/image/orc/text/v2", r.imageToTextPresenterV2.ExtractText)                   // 註冊 POST /api/ai/image/orc/text/v2 路由，處理第二版高併發、Vertical Scale OCR 轉文字請求
	ai.POST("/image/classification/v2", r.imageToClassificationPresenterV2.ClassifyImage) // 註冊 POST /api/ai/image/classification/v2 路由，處理第二版高併發、Vertical Scale圖片分類請求

}

// Router 結構體負責持有所有與路由相關的依賴，主要是各個功能模組的 Presenter
type Router struct {
	imageToTextPresenter             ai.ImageToTextPresenter           // 用於處理圖片轉文字 (OCR) 的 Presenter
	imageToClassificationPresenter   ai.ImageClassificationPresenter   // 用於處理圖片分類的 Presenter
	imageToTextPresenterV2           ai.ImageToTextPresenterV2         // 用於處理第二版高併發、Vertical Scale圖片轉文字 (OCR V2) 的 Presenter
	imageToClassificationPresenterV2 ai.ImageClassificationPresenterV2 // 用於處理第二版高併發、Vertical Scale圖片分類 (Classification V2) 的 Presenter
}

// NewRouter 建構函式用於創建並初始化 Router 實例，依賴注入所有需要的 Presenter
func NewRouter(aiText ai.ImageToTextPresenter, aiClass ai.ImageClassificationPresenter, aiTextV2 ai.ImageToTextPresenterV2, aiClassV2 ai.ImageClassificationPresenterV2) IRouter {
	//func NewRouter(aiText ai.ImageToTextPresenter, aiClass ai.ImageClassificationPresenter,
	// 透過依賴注入的方式傳入各個 Presenter 實例，並返回配置好的 Router 指標
	return &Router{
		imageToTextPresenter:             aiText,    // 初始化 imageToTextPresenter 欄位
		imageToClassificationPresenter:   aiClass,   // 初始化 imageToClassificationPresenter 欄位
		imageToTextPresenterV2:           aiTextV2,  // 初始化 imageToTextPresenterV2 欄位
		imageToClassificationPresenterV2: aiClassV2, // 初始化 imageToClassificationPresenterV2 欄位
	}
}
