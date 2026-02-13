package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// MaxOCRConcurrency 定義最大併發數，避免開啟過多 OCR process 導致伺服器崩潰 (Vertical Scale 防護)
const MaxOCRConcurrency = 4

// ocrSemaphore 使用 Buffered Channel 作為 Semaphore (信號量) 控制併發
var ocrSemaphore = make(chan struct{}, MaxOCRConcurrency)

// ImageToTextPresenterV2 定義 V2 版 OCR 圖片轉文字 Presenter 的介面
// 蔡- 遵循 Go 介面命名慣例，移除 'I' 前綴
type ImageToTextPresenterV2 interface {
	ExtractText(ctx echo.Context) error
}

// imageToTextPresenterV2 實作 ImageToTextPresenterV2 介面
type imageToTextPresenterV2 struct {
	// 可以在此擴充 HTTP Client 或其他配置
}

// NewImageToTextPresenterV2 建立 ImageToTextPresenterV2 的實例
func NewImageToTextPresenterV2() ImageToTextPresenterV2 {
	return &imageToTextPresenterV2{}
}

// ExtractText 執行圖片轉文字 (支援高併發與水平擴展)
// @Summary AI 圖片轉文字
// @description 圖片轉文字 (支援高併發與水平擴展)
// @Tags ai 圖片轉文字
// @version 1.1
// @Accept json multipart/form-data
// @produce json
// @param file formData file true "要上傳的圖片"
// @Success 200 {object} map[string]interface{} "成功時回傳過濾後的 rec_texts 陣列"
// @Failure 400 {object} map[string]string "無法取得圖片"
// @Failure 500 {object} map[string]string "內部錯誤"
// @Failure 503 {object} map[string]string "伺服器忙碌中"
// @Router /api/ai/image/orc/text/v2 [post]
func (p *imageToTextPresenterV2) ExtractText(ctx echo.Context) error {
	// 1. 取得圖片
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "無法取得圖片"})
	}

	src, err := file.Open()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法打開圖片檔案"})
	}
	defer src.Close()

	// 2. 併發控制
	// 嘗試獲取信號量，控制併發請求 (High Concurrency / Backpressure)
	select {
	case ocrSemaphore <- struct{}{}:
		defer func() { <-ocrSemaphore }() // 確保執行完畢後釋放資源
	case <-time.After(5 * time.Second): // 如果等待超過 5 秒，回傳忙碌 (避免請求無限堆積)
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "系統忙碌中，請稍後再試"})
	}

	// 蔡- 使用系統暫存目錄，確保無狀態 (Stateless)，支援水平擴展 (Horizontal Scale)
	// 每個請求都使用獨立的暫存資料夾，避免檔名衝突
	tempDir, err := os.MkdirTemp("", "ocr_task_*")
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法建立暫存目錄"})
	}
	// 確保請求結束後清理所有暫存檔案，防止磁碟空間耗盡
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, file.Filename)
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法建立輸出目錄"})
	}

	// 儲存上傳的圖片到暫存區
	dst, err := os.Create(inputPath)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法儲存圖片"})
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "儲存圖片失敗"})
	}
	dst.Close()

	// 3. 呼叫 PaddX CLI (透過 Context 支援超時控制)
	// 設定 30 秒超時，避免 process 卡死
	reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(reqCtx, "paddlex",
		"--pipeline", "OCR",
		"--input", inputPath,
		"--use_doc_orientation_classify", "False",
		"--use_doc_unwarping", "False",
		"--use_textline_orientation", "False",
		"--save_path", outputDir,
		"--device", "gpu",
	)

	// 執行並捕捉輸出以便除錯
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		// 區分是超時還是執行錯誤
		if reqCtx.Err() == context.DeadlineExceeded {
			return ctx.JSON(http.StatusGatewayTimeout, map[string]string{"error": "OCR 處理逾時"})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error":   "paddx 執行錯誤",
			"details": string(cmdOutput),
		})
	}

	// 4. 讀取 PaddX 的輸出結果
	ext := filepath.Ext(file.Filename)
	nameOnly := strings.TrimSuffix(file.Filename, ext)
	resultFile := filepath.Join(outputDir, nameOnly+"_res.json")

	resultBytes, err := os.ReadFile(resultFile)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法讀取結果 JSON"})
	}

	// 解析結果
	var resultData map[string]any
	if err := json.Unmarshal(resultBytes, &resultData); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "解析 JSON 失敗"})
	}

	// 業務邏輯：過濾信心分數 < 0.85 的文字
	var filteredTexts []string
	if scores, ok := resultData["rec_scores"].([]any); ok {
		if texts, ok := resultData["rec_texts"].([]any); ok {
			for i, s := range scores {
				if scoreFloat, ok := s.(float64); ok && scoreFloat >= 0.85 {
					if i < len(texts) {
						if textStr, ok := texts[i].(string); ok {
							filteredTexts = append(filteredTexts, textStr)
						}
					}
				}
			}
		}
	}
	// 將過濾後資料寫回
	resultData["rec_filtered_texts"] = filteredTexts

	// 讀取視覺化圖片 (Optional)
	visImagePath := filepath.Join(outputDir, nameOnly+"_ocr_res_img"+ext)
	visImageBytes, err := os.ReadFile(visImagePath)
	var visImageBase64 string
	if err == nil {
		visImageBase64 = base64.StdEncoding.EncodeToString(visImageBytes)
	} else {
		fmt.Printf("Warning: reading visualization image failed: %v\n", err)
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"filtered_texts": resultData["rec_filtered_texts"],
		"image_base64":   visImageBase64,
	})
}
