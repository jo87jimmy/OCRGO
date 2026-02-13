package ai

import (
	"OCRGO/internal/pkg/code"
	"bytes"
	"image"
	"io"
	"net/http"

	_ "image/jpeg" // 蔡- 註冊 JPEG 解碼器
	_ "image/png"  // 蔡- 註冊 PNG 解碼器

	"github.com/labstack/echo/v4"
	"github.com/nfnt/resize"
	ort "github.com/yalue/onnxruntime_go"
)

// ImageClassificationPresenter 定義圖片分類 Presenter 的介面 (Basic/V1)
type ImageClassificationPresenter interface {
	ClassifyImage(ctx echo.Context) error
}

// imageClassificationPresenter 實作 ImageClassificationPresenter 介面
type imageClassificationPresenter struct {
	// 蔡- Photo 欄位未使用，但保留結構定義
	Photo []byte `json:"Photo"`
}

// NewImageClassificationPresenter 建立 ImageClassificationPresenter 的實例
func NewImageClassificationPresenter() ImageClassificationPresenter {
	return &imageClassificationPresenter{}
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
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error()))
	}

	// 蔡- 開啟圖片檔案
	// 第一次呼叫 file.Open() 是為了讀取檔案的內容
	multipartFile, err := file.Open()
	if err != nil {
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error()))
	}
	defer multipartFile.Close()

	// 蔡- 讀取圖片數據
	fileData, err := io.ReadAll(multipartFile)
	if err != nil {
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error()))
	}

	// 蔡- 解碼影像資料
	img, _, err := image.Decode(bytes.NewReader(fileData))
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to decode image"})
	}

	// 蔡- 將影像大小調整為 256x256
	resizedImg := resize.Resize(256, 256, img, resize.Lanczos3)

	// 蔡- 將影像轉換為形狀為 [1, 3, 256, 256] 的 float32 數組
	inputData := preprocessImage(resizedImg)

	// 蔡- 初始化 ONNX runtime 環境
	// 注意：在生產環境中，這應該只執行一次 (Singleton)，而不是每個請求都執行
	ort.SetSharedLibraryPath("./onnxruntime.dll")
	err = ort.InitializeEnvironment()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to initialize ONNX environment"})
	}
	defer ort.DestroyEnvironment()

	// Create input tensor
	inputShape := ort.NewShape(1, 3, 256, 256)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create input tensor"})
	}
	defer inputTensor.Destroy()

	// Define output tensor shape
	outputShape := ort.NewShape(1, 11)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create output tensor"})
	}
	defer outputTensor.Destroy()

	// 蔡- 載入模型並建立 Session
	modelPath := "D:/Golang/src/OCR/OCRGO/network.onnx"
	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{"input.1"}, // model's input name
		[]string{"700"},     // model's output name
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		nil,
	)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法初始化 ONNX"})
	}
	defer session.Destroy()

	// 蔡- 運行推理
	err = session.Run()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "推理失敗"})
	}

	// 蔡- 獲取輸出數據
	outputData := outputTensor.GetData()
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
