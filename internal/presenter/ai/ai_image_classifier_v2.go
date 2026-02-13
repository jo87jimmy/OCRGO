package ai // 定義套件名稱為 ai，負責處理與人工智慧相關的邏輯

import (
	"OCRGO/internal/pkg/code" // 引入內部錯誤碼定義套件，用於統一 API 回應格式
	"image"                   // 引入標準影像處理庫，用於解碼與處理圖片
	"log"                     // 引入標準日誌庫，用於記錄系統運行狀態與錯誤
	"net/http"                // 引入 HTTP 協定相關庫，用於處理 HTTP 狀態碼
	"sync"                    // 引入同步原語庫，用於確保併發安全 (如 sync.Once)
	"time"                    // 引入時間庫，用於處理超時控制

	_ "image/jpeg" // 蔡- 註冊 JPEG 解碼器，讓 image.Decode 能識別並解碼 .jpg/.jpeg 格式
	_ "image/png"  // 蔡- 註冊 PNG 解碼器，讓 image.Decode 能識別並解碼 .png 格式

	"github.com/labstack/echo/v4"         // 引入 Echo Web Framework，用於構建存取 API 的 Context
	"github.com/nfnt/resize"              // 引入圖片縮放庫，用於將圖片調整為模型所需的大小
	ort "github.com/yalue/onnxruntime_go" // 引入 ONNX Runtime 的 Go 綁定，用於執行 AI 模型推論
)

// 蔡- 定義最大併發數，避免 CPU/RAM 耗盡 (Vertical Scale)
// 設定同一時間最多允許 8 個請求進行分類，超過的請求將會排隊或被拒絕，防止資源過載
const MaxClassificationConcurrency = 8

// 蔡- 使用 Channel 控制併發請求量 (Semaphore Pattern)
// 建立一個帶緩衝的 Channel 作為信號量，緩衝區大小為 MaxClassificationConcurrency
var classificationSemaphore = make(chan struct{}, MaxClassificationConcurrency)

// 蔡- 保證相關環境只初始化一次 (Singleton Pattern)
// 使用 sync.Once 確保 ONNX 環境初始化的程式碼在整個應用程式生命週期中只執行一次
var (
	onnxInitOnce sync.Once // 用於確保初始化邏輯只執行一次的同步物件
	onnxEnvErr   error     // 儲存初始化過程中可能發生的錯誤，供後續檢查
)

// 蔡- 初始化 ONNX 環境與 Shared Library
// 這是應用程式級別的初始化，負責載入 DLL 與建立環境，不應在每個請求中重複執行以節省開銷
func initONNXEnv() error {
	// 使用 sync.Once 確保匿名函數內的邏輯只被執行一次
	onnxInitOnce.Do(func() {
		// 蔡- 設定 onnxruntime.dll 路徑
		// 指定 ONNX Runtime 的動態連結函式庫位置
		// 建議：實際專案中此路徑應由 Config 注入或自動偵測，目前為硬編碼
		ort.SetSharedLibraryPath("./onnxruntime.dll")

		// 蔡- 初始化環境
		// 呼叫底層 C API 初始化 ONNX Runtime 環境
		err := ort.InitializeEnvironment()
		if err != nil {
			// 若初始化失敗，記錄錯誤日誌
			log.Printf("Failed to initialize ONNX environment: %v", err)
			// 將錯誤儲存於全域變數，供後續判定環境狀態
			onnxEnvErr = err
			return
		}
		// 若初始化成功，記錄成功日誌
		log.Println("ONNX Runtime Environment Initialized Successfully")
	})
	// 回傳初始化結果 (若為 nil 表示成功)
	return onnxEnvErr
}

// ImageClassificationPresenterV2 定義 V2 版高併發、Vertical Scale 圖片分類 Presenter 的介面
type ImageClassificationPresenterV2 interface {
	// ClassifyImage 處理圖片分類的 HTTP 請求
	ClassifyImage(ctx echo.Context) error
}

// imageClassificationPresenterV2 實作 ImageClassificationPresenterV2 介面
// 蔡- 結構體名稱首字母小寫，封裝內部實作細節，避免外部直接依賴具體實作
type imageClassificationPresenterV2 struct {
	// 蔡- 這裡可以存放 Model path 或其他配置
	// 儲存 ONNX 模型檔案的路徑
	ModelPath string
}

// NewImageClassificationPresenterV2 建立 ImageClassificationPresenterV2 的實例
// 蔡- 建構函數名稱明確指出返回的 Presenter 版本，負責依賴注入與初始化設定
func NewImageClassificationPresenterV2() ImageClassificationPresenterV2 {
	// 蔡- 確保環境已初始化
	// 在建立實例時，嘗試初始化 ONNX 環境，確保後續推論可行
	if err := initONNXEnv(); err != nil {
		// 若環境初始化失敗，僅記錄警告，不中斷實例建立 (可能在請求時再重試或報錯)
		log.Printf("Warning: ONNX init failed: %v", err)
	}
	// 返回具體實作結構體的指標，並初始化成員變數
	return &imageClassificationPresenterV2{
		// 蔡- 模型路徑暫時硬編碼，建議未來移至 config
		// 指定使用的 ONNX 模型檔案位置
		ModelPath: "D:/Golang/src/OCR/OCRGO/network.onnx",
	}
}

// ClassifyImage 執行圖片分類 (高併發優化版)
// @Summary AI 圖片分類
// @description 圖片分類 (高併發優化版) - 接收圖片上傳，經過預處理與 ONNX 模型推論，返回分類結果
// @Tags ai 圖片分類
// @version 1.1
// @Accept json multipart/form-data
// @produce json
// @param file formData file true "要上傳的圖片"
// @success 200 object code.SuccessfulMessage{body=string} "成功後返回的值，包含分類結果"
// @failure 400 object code.ErrorMessage{detailed=string} "Bad Request - 請求格式錯誤或圖片無法解析"
// @failure 415 object code.ErrorMessage{detailed=string} "必要欄位帶入錯誤"
// @failure 500 object code.ErrorMessage{detailed=string} "Internal Server Error - 伺服器內部錯誤 (如模型載入失敗)"
// @failure 503 object code.ErrorMessage{detailed=string} "Service Unavailable - 系統忙碌中 (併發限制)"
// @Router /api/ai/image/classification/v2 [post]
func (p *imageClassificationPresenterV2) ClassifyImage(ctx echo.Context) error {
	// 1. 檢查 ONNX 環境是否正常
	// 如果全域環境變數有錯誤，表示 ONNX Runtime 未正確啟動，直接返回 500 錯誤
	if onnxEnvErr != nil {
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.FormatError, "ONNX環境初始化失敗"))
	}

	// 2. 併發控制 (Semaphore)
	// 使用 select 嘗試獲取信號量，進行流量控制
	select {
	case classificationSemaphore <- struct{}{}: // 嘗試寫入 Channel，若 buffer 未滿則成功獲取執行權
		// 使用 defer 確保函式結束時釋放信號量，讓出名額給其他請求
		defer func() { <-classificationSemaphore }()
	case <-time.After(3 * time.Second): // 若等待超過 3 秒仍未獲取執行權
		// 蔡- 若等待過久，回傳 503 Service Unavailable，避免請求積壓導致系統崩潰
		return ctx.JSON(http.StatusServiceUnavailable, code.GetCodeMessage(code.SystemError, "系統忙碌中，請稍後再試"))
	}

	// 3. 獲取並處理圖片 (CPU Bound)
	// 從 HTTP 請求中獲取名為 "file" 的檔案
	file, err := ctx.FormFile("file")
	if err != nil {
		// 若獲取檔案失敗，返回 400 錯誤
		return ctx.JSON(http.StatusBadRequest, code.GetCodeMessage(code.FormatError, err.Error()))
	}

	// 開啟上傳的檔案
	multipartFile, err := file.Open()
	if err != nil {
		// 若開啟檔案失敗，返回 500 錯誤
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.FormatError, err.Error()))
	}
	// 蔡- 確保 multipartFile 關閉
	// 注意：若 image.Decode 發生 panic 或錯誤，這裡的 defer 確保資源釋放
	// 雖然下方有手動 close，但 defer 是防禦性編程的好習慣
	defer multipartFile.Close()

	// 解碼圖片，將檔案串流轉換為 image.Image 物件
	// 這裡會依據 import 的 _ "image/jpeg" 或 _ "image/png" 自動識別格式
	img, _, err := image.Decode(multipartFile)
	if err != nil {
		// 若圖片解碼失敗 (例如非圖片格式)，返回 400 錯誤
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to decode image"})
	}

	// 4. 前處理
	// 將圖片調整大小為模型輸入要求的 256x256 像素
	// 使用 resize.Lanczos3 演算法進行高品質縮放
	resizedImg := resize.Resize(256, 256, img, resize.Lanczos3)
	// 呼叫輔助函式將圖片轉換為模型所需的正規化數據 (float32 array)
	inputData := preprocessImage(resizedImg)

	// 5. 執行推論 (Inference)
	// 蔡- Initialize Input Tensor
	// 定義輸入張量的形狀: Batch Size=1, Channels=3, Height=256, Width=256
	inputShape := ort.NewShape(1, 3, 256, 256)
	// 根據形狀與數據建立輸入 Tensor
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		// 若 Tensor 建立失敗，返回 500 錯誤
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "Failed to create input tensor"))
	}
	// 確保 Tensor 使用完畢後釋放記憶體
	defer inputTensor.Destroy()

	// Initialize Output Tensor
	// 定義輸出張量的形狀: Batch Size=1, Classes=11 (共有 11 個分類)
	outputShape := ort.NewShape(1, 11)
	// 建立一個空的輸出 Tensor 來接收模型推論結果
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		// 若 Tensor 建立失敗，返回 500 錯誤
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "Failed to create output tensor"))
	}
	// 確保 Tensor 使用完畢後釋放記憶體
	defer outputTensor.Destroy()

	// 建立 Session
	// 蔡- 注意：每次請求都建立 Session 開銷較大，但在併發受限 (Max=8) 下尚可接受。
	// 理想情況應復用 Session (Singleton) 或使用 Session Pool 以提升效能。
	// 參數說明：模型路徑, 輸入節點名稱, 輸出節點名稱, 輸入 Tensor, 輸出 Tensor
	session, err := ort.NewAdvancedSession(
		p.ModelPath,
		[]string{"input.1"}, // 模型輸入層名稱 (需與模型定義一致)
		[]string{"700"},     // 模型輸出層名稱 (需與模型定義一致)
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		nil, // 選項參數
	)
	if err != nil {
		// 若 Session 建立失敗，記錄錯誤並返回 500
		log.Printf("Session creation error: %v", err)
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "無法載入模型 session"))
	}
	// 確保 Session 使用完畢後銷毀
	defer session.Destroy()

	// 運行推理 (Run Inference)
	// 執行模型計算，將結果寫入 outputTensor
	err = session.Run()
	if err != nil {
		// 若推論過程發生錯誤，返回 500
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "推理失敗"))
	}

	// 獲取推論結果的數據 (float32 slice)
	outputData := outputTensor.GetData()

	// 6. 後處理與回傳
	// 定義分類標籤，對應模型的 11 個輸出類別
	classLabels := []string{
		"麵包", "乳製品", "點心", "蛋", "油炸食品", "肉", "義大利麵", "米", "海鮮", "湯", "蔬果",
	}
	// 設定信心閾值，低於此值的結果視為不可靠
	threshold := float32(4.5)

	allBelowThreshold := true // 標記是否所有分數都低於閾值
	maxIndex := 0             // 記錄最高分的索引
	maxScore := outputData[0] // 記錄最高分，初始化為第一個元素

	// 遍歷輸出數據，找出最高分及其索引
	for i, score := range outputData {
		// 若有任一分數大於等於閾值，則標記為否
		if score >= threshold {
			allBelowThreshold = false
		}
		// 更新最高分與索引
		if score > maxScore {
			maxScore = score
			maxIndex = i
		}
	}

	var predictedClass string
	// 若所有分數都低於閾值，判定為無法辨識
	if allBelowThreshold {
		predictedClass = "無法辨識"
	} else {
		// 否則取最高分對應的標籤作為預測結果
		predictedClass = classLabels[maxIndex]
	}

	// 返回 HTTP 200 OK 與 JSON 格式的預測結果
	return ctx.JSON(http.StatusOK, map[string]any{"result": predictedClass})
}

// preprocessImage 將影像預處理成歸一化的 float32 數組 (0-1)
// 輸入：Go 的 image.Image 物件
// 輸出：展平的 float32 切片 (CHW 格式：先 R 通道，再 G，再 B)
func preprocessImage(img image.Image) []float32 {
	// 獲取圖片邊界
	bounds := img.Bounds()
	// 獲取圖片寬高
	width, height := bounds.Max.X, bounds.Max.Y
	// 建立輸出陣列，大小為 Channels(3) * Height(256) * Width(256)
	output := make([]float32, 1*3*256*256)

	// 遍歷每個像素 (高度 y)
	for y := range height {
		// 遍歷每個像素 (寬度 x)
		for x := range width {
			// 獲取該座標的 RGBA 值 (範圍 0-65535)
			r, g, b, _ := img.At(x, y).RGBA()

			// 計算在平面陣列中的索引位置
			index := y*width + x

			// 蔡- RGBA() 返回 16-bit 範圍，需要右移 8 位轉為 8-bit (0-255)
			// 然後除以 255.0 進行歸一化 (Normalization) 到 0.0-1.0 區間
			// R 通道數據
			output[index] = float32(r>>8) / 255.0
			// G 通道數據 (偏移 256*256)
			output[index+256*256] = float32(g>>8) / 255.0
			// B 通道數據 (偏移 2 * 256*256)
			output[index+2*256*256] = float32(b>>8) / 255.0
		}
	}
	// 返回處理後的數據
	return output
}
