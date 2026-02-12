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

type IImageClassificationPresenter interface {
	ClassifyImage(ctx echo.Context) error
}

type presenter struct {
	Photo []byte `json:"Photo"` // Ensure this is base64 decoded
}

func NewImageClassification() IImageClassificationPresenter {
	return &presenter{}
}

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
func (p *presenter) ClassifyImage(ctx echo.Context) error {
	//蔡- 獲取圖片
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error()))
	}

	//蔡- 開啟圖片檔案
	//蔡- 第一次呼叫 file.Open() 是為了讀取檔案的內容。 您需要讀取文件的內容來將其儲存到記憶體中，以便進一步處理和操作。
	multipartFile, err := file.Open()
	if err != nil {
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error()))
	}

	//蔡- 讀取圖片數據
	fileData, err := io.ReadAll(multipartFile)
	if err != nil {
		return ctx.JSON(http.StatusOK, code.GetCodeMessage(code.FormatError, err.Error()))
	}
	defer multipartFile.Close()

	//蔡- 解碼影像資料
	img, _, err := image.Decode(bytes.NewReader(fileData))
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to decode image"})
	}

	//蔡- 將影像大小調整為 256x256
	resizedImg := resize.Resize(256, 256, img, resize.Lanczos3)

	//蔡- 將影像轉換為形狀為 [1, 3, 256, 256] 的 float32 數組
	inputData := preprocessImage(resizedImg) // Implement this to extract pixel values

	//蔡- Initialize the ONNX runtime environment
	// Set the path to the ONNX Runtime shared library
	// 取得 onnxruntime.dll 的相對路徑
	// dllPath, _ := filepath.Abs("onnxruntime.dll")
	// ort.SetSharedLibraryPath(dllPath)
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

	// Define output tensor shape (example: [1, 11])
	outputShape := ort.NewShape(1, 11) // Adjust this based on your model's output
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create output tensor"})
	}
	defer outputTensor.Destroy()

	//蔡- 注意:請確保替換 input_name 和 output_name 為你的模型的實際輸入和輸出名稱。你可以使用 ONNX 的工具來查看模型的輸入和輸出名稱，例如：
	//蔡- 先到 https://github.com/lutzroeder/Netron?tab=readme-ov-file 中下載 Netron
	modelPath := "D:/Golang/src/OCR/OCRGO/network.onnx"
	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{"input.1"}, // Replace with your model's input name
		[]string{"700"},     // Replace with your model's output name
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		nil,
	)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法初始化 ONNX"})
	}
	defer session.Destroy()

	//- 運行推理
	err = session.Run()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "推理失敗"})
	}
	//蔡- 獲取輸出數據
	outputData := outputTensor.GetData()
	//蔡- 定義類別標籤列表
	classLabels := []string{
		"麵包", "乳製品", "點心", "蛋", "油炸食品", "肉", "義大利麵", "米", "海鮮", "湯", "蔬果",
	}
	//蔡- Define the threshold
	threshold := float32(4.5)

	//蔡- Check if all scores are below the threshold
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

	// Map the index to the corresponding class label or set to "無法辨識"
	var predictedClass string
	if allBelowThreshold {
		predictedClass = "無法辨識"
	} else {
		predictedClass = classLabels[maxIndex]
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{"result": predictedClass})
}

// Helper function: preprocess the image into a normalized float32 array
func preprocessImage(img image.Image) []float32 {
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y

	// 初始化輸出數組 [1, 3, 256, 256]
	output := make([]float32, 1*3*256*256) // Adjust dimensions as needed

	// 遍歷圖像像素
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(x, y).RGBA()

			// 將 uint32 轉換為 float32 並進行標準化 (0-1)
			index := y*width + x
			output[index] = float32(r>>8) / 255.0
			output[index+256*256] = float32(g>>8) / 255.0
			output[index+2*256*256] = float32(b>>8) / 255.0
		}
	}
	return output
}
