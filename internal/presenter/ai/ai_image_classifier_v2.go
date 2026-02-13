package ai

import (
	"OCRGO/internal/pkg/code"
	"image"
	"log"
	"net/http"
	"sync"
	"time"

	_ "image/jpeg" // 蔡- 註冊 JPEG 解碼器
	_ "image/png"  // 蔡- 註冊 PNG 解碼器

	"github.com/labstack/echo/v4"
	"github.com/nfnt/resize"
	ort "github.com/yalue/onnxruntime_go"
)

// 蔡- 定義最大併發數，避免 CPU/RAM 耗盡 (Vertical Scale)
const MaxClassificationConcurrency = 8

// 蔡- 使用 Channel 控制併發請求量 (Semaphore Pattern)
var classificationSemaphore = make(chan struct{}, MaxClassificationConcurrency)

// 蔡- 保證相關環境只初始化一次 (Singleton Pattern)
var (
	onnxInitOnce sync.Once
	onnxEnvErr   error
)

// 蔡- 初始化 ONNX 環境與 Shared Library
// 這是應用程式級別的初始化，不應在每個請求中重複執行
func initONNXEnv() error {
	onnxInitOnce.Do(func() {
		// 蔡- 設定 onnxruntime.dll 路徑
		// 建議：實際專案中此路徑應由 Config 注入或自動偵測
		ort.SetSharedLibraryPath("./onnxruntime.dll")

		// 蔡- 初始化環境
		err := ort.InitializeEnvironment()
		if err != nil {
			log.Printf("Failed to initialize ONNX environment: %v", err)
			onnxEnvErr = err
			return
		}
		log.Println("ONNX Runtime Environment Initialized Successfully")
	})
	return onnxEnvErr
}

// ImageClassificationPresenterV2 定義 V2 版圖片分類 Presenter 的介面
// 蔡- 遵循 Go 介面命名慣例，移除 'I' 前綴
type ImageClassificationPresenterV2 interface {
	ClassifyImage(ctx echo.Context) error
}

// imageClassificationPresenterV2 實作 ImageClassificationPresenterV2 介面
// 蔡- 結構體名稱首字母小寫，封裝內部實作細節
type imageClassificationPresenterV2 struct {
	// 蔡- 這裡可以存放 Model path 或其他配置
	ModelPath string
}

// NewImageClassificationPresenterV2 建立 ImageClassificationPresenterV2 的實例
// 蔡- 建構函數名稱明確指出返回的 Presenter 版本
func NewImageClassificationPresenterV2() ImageClassificationPresenterV2 {
	// 蔡- 確保環境已初始化
	if err := initONNXEnv(); err != nil {
		log.Printf("Warning: ONNX init failed: %v", err)
	}
	return &imageClassificationPresenterV2{
		// 蔡- 模型路徑暫時硬編碼，建議未來移至 config
		ModelPath: "D:/Golang/src/OCR/OCRGO/network.onnx",
	}
}

// ClassifyImage 執行圖片分類 (高併發優化版)
// @Summary AI 圖片分類
// @description 圖片分類 (高併發優化版)
// @Tags ai 圖片分類
// @version 1.1
// @Accept json multipart/form-data
// @produce json
// @param file formData file true "要上傳的圖片"
// @success 200 object code.SuccessfulMessage{body=string} "成功後返回的值"
// @failure 400 object code.ErrorMessage{detailed=string} "Bad Request"
// @failure 415 object code.ErrorMessage{detailed=string} "必要欄位帶入錯誤"
// @failure 500 object code.ErrorMessage{detailed=string} "Internal Server Error"
// @failure 503 object code.ErrorMessage{detailed=string} "Service Unavailable"
// @Router /api/ai/image/classification/v2 [post]
func (p *imageClassificationPresenterV2) ClassifyImage(ctx echo.Context) error {
	// 1. 檢查 ONNX 環境是否正常
	if onnxEnvErr != nil {
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.FormatError, "ONNX環境初始化失敗"))
	}

	// 2. 併發控制 (Semaphore)
	select {
	case classificationSemaphore <- struct{}{}:
		defer func() { <-classificationSemaphore }()
	case <-time.After(3 * time.Second):
		// 蔡- 若等待過久，回傳 503 Service Unavailable，避免請求積壓
		return ctx.JSON(http.StatusServiceUnavailable, code.GetCodeMessage(code.SystemError, "系統忙碌中，請稍後再試"))
	}

	// 3. 獲取並處理圖片 (CPU Bound)
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, code.GetCodeMessage(code.FormatError, err.Error()))
	}

	multipartFile, err := file.Open()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.FormatError, err.Error()))
	}
	// 蔡- 確保 multipartFile 關閉
	// 注意：若 image.Decode 發生 panic 或錯誤，這裡的 defer 確保資源釋放
	// 但稍後我們手動 close 以便盡早釋放
	defer multipartFile.Close()

	img, _, err := image.Decode(multipartFile)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to decode image"})
	}

	// 4. 前處理
	resizedImg := resize.Resize(256, 256, img, resize.Lanczos3)
	inputData := preprocessImage(resizedImg)

	// 5. 執行推論 (Inference)
	// 蔡- Initialize Input Tensor
	inputShape := ort.NewShape(1, 3, 256, 256)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "Failed to create input tensor"))
	}
	defer inputTensor.Destroy()

	// Initialize Output Tensor
	outputShape := ort.NewShape(1, 11)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "Failed to create output tensor"))
	}
	defer outputTensor.Destroy()

	// 建立 Session
	// 蔡- 注意：每次請求都建立 Session 開銷較大，但在併發受限下尚可接受。
	// 理想情況應復用 Session 或使用 Session Pool。
	session, err := ort.NewAdvancedSession(
		p.ModelPath,
		[]string{"input.1"},
		[]string{"700"},
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		nil,
	)
	if err != nil {
		log.Printf("Session creation error: %v", err)
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "無法載入模型 session"))
	}
	defer session.Destroy()

	// 運行推理
	err = session.Run()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, code.GetCodeMessage(code.SystemError, "推理失敗"))
	}

	outputData := outputTensor.GetData()

	// 6. 後處理與回傳
	classLabels := []string{
		"麵包", "乳製品", "點心", "蛋", "油炸食品", "肉", "義大利麵", "米", "海鮮", "湯", "蔬果",
	}
	threshold := float32(4.5)

	allBelowThreshold := true
	maxIndex := 0
	maxScore := outputData[0]
	for i, score := range outputData {
		if score >= threshold {
			allBelowThreshold = false
		}
		if score > maxScore {
			maxScore = score
			maxIndex = i
		}
	}

	var predictedClass string
	if allBelowThreshold {
		predictedClass = "無法辨識"
	} else {
		predictedClass = classLabels[maxIndex]
	}

	return ctx.JSON(http.StatusOK, map[string]any{"result": predictedClass})
}

// preprocessImage 將影像預處理成歸一化的 float32 數組 (0-1)
func preprocessImage(img image.Image) []float32 {
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	output := make([]float32, 1*3*256*256)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			index := y*width + x
			// 蔡- RGBA() 返回 16-bit 範圍，需要右移 8 位轉為 8-bit 並歸一化
			output[index] = float32(r>>8) / 255.0
			output[index+256*256] = float32(g>>8) / 255.0
			output[index+2*256*256] = float32(b>>8) / 255.0
		}
	}
	return output
}
