package ai // 定義 ai 套件，負責處理 AI 相關的業務邏輯

import (
	"OCRGO/internal/pkg/code" // 引入內部的 code 套件，用於處理統一的錯誤碼與訊息
	"bytes"                   // 引入 bytes 套件，用於操作 byte slice 緩衝區
	"image"                   // 引入 image 套件，提供基本的影像處理介面
	"io"                      // 引入 io 套件，用於進行 I/O 操作 (如讀取檔案)
	"net/http"                // 引入 net/http 套件，提供 HTTP 客戶端與伺服器功能

	_ "image/jpeg" // 蔡- 註冊 JPEG 解碼器，讓 image.Decode 能支援 JPEG 格式
	_ "image/png"  // 蔡- 註冊 PNG 解碼器，讓 image.Decode 能支援 PNG 格式

	"github.com/labstack/echo/v4"         // 引入 Echo Web 框架，用於處理 HTTP 請求
	"github.com/nfnt/resize"              // 引入 resize 套件，用於調整圖片尺寸
	ort "github.com/yalue/onnxruntime_go" // 引入 ONNX Runtime Go 綁定，用於執行 ONNX 模型
)

// ImageClassificationPresenter 定義圖片分類 Presenter 的介面 (Basic/V1)
type ImageClassificationPresenter interface {
	ClassifyImage(ctx echo.Context) error // ClassifyImage 定義分類圖片的方法，接收 Echo Context 並返回錯誤
}

// imageClassificationPresenter 實作 ImageClassificationPresenter 介面
type imageClassificationPresenter struct {
	// 蔡- Photo 欄位未使用，但保留結構定義
	Photo []byte `json:"Photo"` // Photo 欄位用於儲存圖片的 byte 數據，對應 JSON 中的 "Photo" 欄位
}

// NewImageClassificationPresenter 建立 ImageClassificationPresenter 的實例
func NewImageClassificationPresenter() ImageClassificationPresenter {
	return &imageClassificationPresenter{} // 建立並返回 imageClassificationPresenter 的指針實例
}

// ClassifyImage 執行圖片分類
// @Summary AI 圖片分類
// @description 圖片分類
// @Tags ai 圖片分類
// @version 1.0
// @Accept json multipart/form-data
// @produce json
// @param file formData file true "要上傳的圖片"
// @success 200 object code.SuccessfulMessage{body=string} "成功後返回的值"
// @failure 400 object code.ErrorMessage{detailed=string} "Bad Request"
// @failure 415 object code.ErrorMessage{detailed=string} "必要欄位帶入錯誤"
// @failure 500 object code.ErrorMessage{detailed=string} "Internal Server Error"
// @Router /api/ai/image/classification [post]
func (p *imageClassificationPresenter) ClassifyImage(ctx echo.Context) error {
	// 蔡- 獲取圖片
	file, err := ctx.FormFile("file") // 從請求的 Form Data 中獲取名為 "file" 的檔案
	if err != nil {                   // 如果獲取檔案失敗 (例如未上傳檔案)
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error())) // 返回 200 OK 並附帶錯誤訊息 (這裡使用 200 可能是專案慣例)
	}

	// 蔡- 開啟圖片檔案
	// 第一次呼叫 file.Open() 是為了讀取檔案的內容
	multipartFile, err := file.Open() // 開啟上傳的檔案以讀取內容
	if err != nil {                   // 如果開啟檔案失敗
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error())) // 返回 200 OK 並附帶錯誤訊息
	}
	defer multipartFile.Close() // 使用 defer 確保函式執行完畢後關閉檔案

	// 蔡- 讀取圖片數據
	fileData, err := io.ReadAll(multipartFile) // 將檔案內容全部讀取到記憶體中 (byte slice)
	if err != nil {                            // 如果讀取檔案失敗
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error())) // 返回 200 OK 並附帶錯誤訊息
	}

	// 蔡- 解碼影像資料
	img, _, err := image.Decode(bytes.NewReader(fileData)) // 將 byte 數據解碼為 image.Image 物件
	if err != nil {                                        // 如果解碼失敗 (例如非圖片格式)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to decode image"}) // 返回 400 Bad Request 錯誤
	}

	// 蔡- 將影像大小調整為 256x256
	resizedImg := resize.Resize(256, 256, img, resize.Lanczos3) // 使用 Lanczos3 演算法將圖片調整為 256x256 像素

	// 蔡- 將影像轉換為形狀為 [1, 3, 256, 256] 的 float32 數組
	inputData := preprocessImage(resizedImg) // 呼叫預處理函數將圖片轉換為模型所需的輸入格式 (應在同 package 中定義)

	// 蔡- 初始化 ONNX runtime 環境
	// 注意：在生產環境中，這應該只執行一次 (Singleton)，而不是每個請求都執行
	ort.SetSharedLibraryPath("./onnxruntime.dll") // 設定 ONNX Runtime 的動態連結庫路徑
	err = ort.InitializeEnvironment()             // 初始化 ONNX Runtime 環境
	if err != nil {                               // 如果初始化環境失敗
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to initialize ONNX environment"}) // 返回 500 Internal Server Error
	}
	defer ort.DestroyEnvironment() // 使用 defer 確保函式執行完畢後銷毀環境

	// Create input tensor
	inputShape := ort.NewShape(1, 3, 256, 256)               // 定義輸入張量的形狀 (Batch=1, Channels=3, Height=256, Width=256)
	inputTensor, err := ort.NewTensor(inputShape, inputData) // 根據形狀和數據建立輸入張量
	if err != nil {                                          // 如果建立輸入張量失敗
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create input tensor"}) // 返回 500 Internal Server Error
	}
	defer inputTensor.Destroy() // 使用 defer 確保函式執行完畢後銷毀輸入張量

	// Define output tensor shape
	outputShape := ort.NewShape(1, 11)                            // 定義輸出張量的形狀 (Batch=1, Classes=11)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape) // 建立一個空的輸出張量來接收結果
	if err != nil {                                               // 如果建立輸出張量失敗
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create output tensor"}) // 返回 500 Internal Server Error
	}
	defer outputTensor.Destroy() // 使用 defer 確保函式執行完畢後銷毀輸出張量

	// 蔡- 載入模型並建立 Session
	modelPath := "D:/Golang/src/OCR/OCRGO/network.onnx" // 設定 ONNX 模型檔案的絕對路徑
	session, err := ort.NewAdvancedSession(             // 建立進階的推理 Session
		modelPath,                 // 模型路徑
		[]string{"input.1"},       // 模型的輸入節點名稱
		[]string{"700"},           // 模型的輸出節點名稱
		[]ort.Value{inputTensor},  // 輸入張量列表
		[]ort.Value{outputTensor}, // 輸出張量列表
		nil,                       // 進階選項 (此處為 nil)
	)
	if err != nil { // 如果建立 Session 失敗
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法初始化 ONNX"}) // 返回 500 Internal Server Error
	}
	defer session.Destroy() // 使用 defer 確保函式執行完畢後銷毀 Session

	// 蔡- 運行推理
	err = session.Run() // 執行模型推理
	if err != nil {     // 如果推理過程中發生錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "推理失敗"}) // 返回 500 Internal Server Error
	}

	// 蔡- 獲取輸出數據
	outputData := outputTensor.GetData() // 從輸出張量中獲取推理結果數據 (float32 slice)
	classLabels := []string{             // 定義類別標籤的 slice，對應模型的輸出索引
		"麵包", "乳製品", "點心", "蛋", "油炸食品", "肉", "義大利麵", "米", "海鮮", "湯", "蔬果",
	}
	threshold := float32(4.5) // 設定判斷的信心度閾值

	allBelowThreshold := true          // 初始化變數，標記是否所有類別分數都低於閾值
	maxIndex := 0                      // 初始化變數，記錄最高分的索引
	maxScore := outputData[0]          // 初始化變數，記錄最高分的分數，預設為第一個
	for i, score := range outputData { // 遍歷輸出的每個分類分數
		if score >= threshold { // 如果有分數大於或等於閾值
			allBelowThreshold = false // 標記為否 (即有可信的結果)
		}
		if score > maxScore { // 如果當前分數大於目前記錄的最高分
			maxScore = score // 更新最高分
			maxIndex = i     // 更新最高分索引
		}
	}

	var predictedClass string // 定義變數儲存最終預測的類別名稱
	if allBelowThreshold {    // 如果所有分數都低於閾值
		predictedClass = "無法辨識" // 判定結果為 "無法辨識"
	} else {
		predictedClass = classLabels[maxIndex] // 否則取最高分對應的標籤
	}

	return ctx.JSON(http.StatusOK, map[string]any{"result": predictedClass}) // 返回 200 OK 及 JSON 格式的預測結果
}
