package main // 定義套件名稱為 main，這是 Go 語言應用程式的執行入口點

import (
	"OCRGO/internal/pkg/util" // 引入工具包，用於讀取環境變數、配置與通用功能
	"OCRGO/internal/router"   // 引入路由管理模組，負責定義與管理所有的 API 路徑

	_ "OCRGO/docs"                            // 引入 Swagger 文檔生成的副作用 (side-effect import)，確保 API 文檔能夠正確生成與顯示
	presenterAi "OCRGO/internal/presenter/ai" // 引入 AI 相關的業務邏輯層 (Presenter)，並命名別名為 presenterAi 以增加可讀性

	"github.com/labstack/echo/v4" // 引入 Echo Web 框架 (v4)，用於構建高效能的 HTTP 伺服器
)

// Swagger API 文檔註解區塊
// @title           OCRGO API
// @version         1.0
// @description     OCR API 服務，提供圖片轉文字與圖片分類功能
// @contact.name    小蔡資訊
// @contact.url     https://jo87jimmy.github.io/
// @contact.email   jo87jimmy@gmail.com
// @host            localhost:9541
// @BasePath        /

// Swagger 文檔訪問地址: http://127.0.0.1:9541/api/swagger/

// main 程式主入口函數
func main() {
	// 初始化 Echo 實例，這是整個 Web 應用程式的核心對象
	route := echo.New()

	// 初始化業務邏輯依賴 (Dependency Injection)
	// 實例化圖片轉文字 (OCR) 的 Presenter (V1 版本)，封裝具體的 OCR 處理邏輯
	presenterText := presenterAi.NewImageToTextPresenter()
	// 實例化圖片轉文字 (OCR) 的 Presenter (V2 版本)，高併發、Vertical Scale
	presenterTextV2 := presenterAi.NewImageToTextPresenterV2()
	// 實例化圖片分類的 Presenter (V1 版本)，封裝圖片分類的業務邏輯
	presenterClass := presenterAi.NewImageClassificationPresenter()
	// 實例化圖片分類的 Presenter (V2 版本)，高併發、Vertical Scale
	presenterClassV2 := presenterAi.NewImageClassificationPresenterV2()

	// 初始化路由管理器，並將所有的 Presenter 依賴注入到路由器中
	// 將路由層與業務邏輯層解耦，便於測試與維護
	router := router.NewRouter(presenterText, presenterClass, presenterTextV2, presenterClassV2)
	// router := router.NewRouter(presenterText, presenterClass, presenterTextV2)
	// 註冊所有 API 路由路徑到 Echo 實例中
	router.InitRoutes(route)

	// 啟動 HTTP 伺服器
	// 從 util 工具包中讀取環境變數配置的 PORT，增加部署的靈活性
	// 使用 Logger.Fatal 確保如果服務啟動失敗（如端口衝突），會記錄錯誤日誌並退出程式
	route.Logger.Fatal(route.Start(":" + util.Source["ENV"]["PORT"]))
}
