package ai

import (
	"context"         // 用於處理請求的上下文，包含超時控制與取消信號
	"encoding/base64" // 用於將圖片編碼為 Base64 字串，以便透過 JSON 回傳給前端
	"encoding/json"   // 用於解析 PaddX 輸出的 JSON 結果檔案
	"fmt"             // 用於格式化輸出日誌或錯誤訊息
	"io"              // 用於檔案讀寫與串流操作
	"net/http"        // 用於 HTTP 狀態碼與相關常數
	"os"              // 用於作業系統級別的檔案操作 (建立目錄、讀取檔案等)
	"os/exec"         // 用於執行外部指令 (此處用於呼叫 PaddX CLI)
	"path/filepath"   // 用於跨平台的檔案路徑處理
	"strings"         // 用於字串處理 (如檔名分割)
	"time"            // 用於設定超時時間與時間相關操作

	"github.com/labstack/echo/v4" // Web Framework，用於處理 HTTP 請求與回應
)

// MaxOCRConcurrency 定義最大併發數
// 用途：限制同時執行的 OCR 任務數量，防止過多 Goroutine 或外部 Process 耗盡 CPU/GPU 資源。
// 架構考量：這是 Vertical Scale (垂直擴展) 的防護機制，避免單一伺服器因負載過重而崩潰 (Throttling)。
const MaxOCRConcurrency = 4

// ocrSemaphore 使用 Buffered Channel 作為 Semaphore (信號量) 控制併發
// 用途：透過 Channel 的緩衝區大小來實作計數信號量。
// 架構考量：這是一種 Backpressure (背壓) 機制，當系統忙碌時拒絕過多請求，保護系統穩定性。
var ocrSemaphore = make(chan struct{}, MaxOCRConcurrency)

// ImageToTextPresenterV2 定義 V2 版 OCR 圖片轉文字 Presenter 的介面
// 用途：定義對外的合約 (Contract)，解耦實作與呼叫端。
// 架構考量：符合依賴反轉原則 (DIP)，方便未來替換實作或進行單元測試 (Mocking)。
// 命名慣例：遵循 Go 介面命名慣例，移除 'I' 前綴 (Idiomatic Go)。
type ImageToTextPresenterV2 interface {
	ExtractText(ctx echo.Context) error
}

// imageToTextPresenterV2 實作 ImageToTextPresenterV2 介面
// 用途：具體的實作結構體，負責處理圖片轉文字的業務邏輯。
type imageToTextPresenterV2 struct {
	// 擴充點：可以在此擴充 HTTP Client、Logger 或其他配置 (Dependency Injection)。
}

// NewImageToTextPresenterV2 建立 ImageToTextPresenterV2 的實例
// 用途：工廠函數 (Factory Function)，用於初始化並回傳 Presenter 實例。
// 架構考量：隱藏具體實作細節，僅暴露介面給外部使用。
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
	// 用途：從 HTTP Multipart Form Data 中讀取上傳的檔案。
	file, err := ctx.FormFile("file")
	if err != nil {
		// 錯誤處理：若無法讀取檔案，回傳 400 Bad Request。
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "無法取得圖片"})
	}

	// 用途：打開上傳的檔案串流。
	src, err := file.Open()
	if err != nil {
		// 錯誤處理：若無法打開檔案，回傳 500 Internal Server Error。
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法打開圖片檔案"})
	}
	// 資源釋放：確保函數結束時關閉檔案串流，避免 Memory Leak。
	defer src.Close()

	// 2. 併發控制
	// 用途：嘗試獲取信號量，控制併發請求 (High Concurrency / Backpressure)。
	select {
	case ocrSemaphore <- struct{}{}:
		// 成功獲取信號量，進入臨界區 (Critical Section)。
		// 確保執行完畢後釋放信號量，讓其他請求可以進入。
		defer func() { <-ocrSemaphore }()
	case <-time.After(5 * time.Second):
		// 超時處理：如果等待超過 5 秒無法獲取信號量，則判定系統忙碌。
		// 架構考量：Fail Fast 機制，避免請求在 Queue 中無限堆積導致客戶端長時間等待或連線超時。
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "系統忙碌中，請稍後再試"})
	}

	// 3. 建立暫存環境
	// 用途：使用系統暫存目錄建立獨立的工作區。
	// 架構考量：確保無狀態 (Stateless)，每個請求獨立處理，避免檔名衝突，並支援水平擴展 (Horizontal Scale)。
	tempDir, err := os.MkdirTemp("", "ocr_task_*")
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法建立暫存目錄"})
	}
	// 清理機制：確保請求結束後清理所有暫存檔案，防止磁碟空間耗盡 (Disk Exhaustion)。
	defer os.RemoveAll(tempDir)

	// 設定輸入與輸出路徑
	inputPath := filepath.Join(tempDir, file.Filename)
	outputDir := filepath.Join(tempDir, "output")
	// 建立輸出目錄，權限設為 0755 (Owner 可讀寫執行，Group/Others 可讀執行)。
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法建立輸出目錄"})
	}

	// 4. 儲存檔案
	// 用途：在暫存區建立目標檔案。
	dst, err := os.Create(inputPath)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法儲存圖片"})
	}
	// 用途：將上傳的檔案內容複製到暫存檔案。
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close() // 複製失敗時需先關閉檔案再回傳錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "儲存圖片失敗"})
	}
	dst.Close() // 成功複製後關閉檔案

	// 5. 呼叫 PaddX CLI (外部進程調用)
	// 用途：設定 Context 超時控制。
	// 架構考量：設定 30 秒硬性超時 (Hard Timeout)，避免外部 Process 卡死導致 Goroutine 洩漏 (Leak)。
	reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), 30*time.Second)
	defer cancel() // 確保 Context 資源釋放

	// 建構指令：呼叫 paddlex 進行 OCR 辨識。
	// 參數說明：
	// --pipeline OCR: 指定使用 OCR 處理流程
	// --input: 輸入圖片路徑
	// --save_path: 結果與圖片輸出路徑
	// --device gpu: 強制使用 GPU 加速 (效能優化)
	cmd := exec.CommandContext(reqCtx, "paddlex",
		"--pipeline", "OCR",
		"--input", inputPath,
		"--use_doc_orientation_classify", "False",
		"--use_doc_unwarping", "False",
		"--use_textline_orientation", "False",
		"--save_path", outputDir,
		"--device", "gpu",
	)

	// 執行並捕捉輸出：CombinedOutput 會回傳 Standard Output 和 Standard Error。
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		// 錯誤分類：區分是「超時」還是「執行錯誤」。
		if reqCtx.Err() == context.DeadlineExceeded {
			// 若 Context 逾時，回傳 504 Gateway Timeout。
			return ctx.JSON(http.StatusGatewayTimeout, map[string]string{"error": "OCR 處理逾時"})
		}
		// 若是其他執行錯誤，回傳 500 並附上 CLI 輸出日誌以便除錯。
		return ctx.JSON(http.StatusInternalServerError, map[string]any{
			"error":   "paddx 執行錯誤",
			"details": string(cmdOutput),
		})
	}

	// 6. 讀取 PaddX 的輸出結果
	// 用途：計算預期的結果檔案名稱。Paddlex 通常會輸出 JSON 檔案。
	ext := filepath.Ext(file.Filename)
	nameOnly := strings.TrimSuffix(file.Filename, ext)
	resultFile := filepath.Join(outputDir, nameOnly+"_res.json")

	// 讀取結果檔案內容
	resultBytes, err := os.ReadFile(resultFile)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法讀取結果 JSON"})
	}

	// 解析 JSON 結果到 Map 中
	var resultData map[string]any
	if err := json.Unmarshal(resultBytes, &resultData); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "解析 JSON 失敗"})
	}

	// 7. 業務邏輯處理
	// 用途：過濾信心分數 (Confidence Score) 低於 0.85 的文字，提升資料品質。
	var filteredTexts []string

	// 類型斷言 (Type Assertion)：安全地存取 JSON 結構。
	if scores, ok := resultData["rec_scores"].([]any); ok {
		if texts, ok := resultData["rec_texts"].([]any); ok {
			// 遍歷所有辨識結果的分數
			for i, s := range scores {
				// 檢查分數是否大於等於 0.85
				if scoreFloat, ok := s.(float64); ok && scoreFloat >= 0.85 {
					// 確保索引不越界
					if i < len(texts) {
						// 取出對應的文字並加入過濾後的列表
						if textStr, ok := texts[i].(string); ok {
							filteredTexts = append(filteredTexts, textStr)
						}
					}
				}
			}
		}
	}
	// 將過濾後的文字列表寫回結果 Map
	resultData["rec_filtered_texts"] = filteredTexts

	// 8. 讀取視覺化圖片 (Optional)
	// 用途：讀取 PaddX 產生的標註圖片，回傳給前端顯示 (如加上紅色框框的 OCR 結果圖)。
	visImagePath := filepath.Join(outputDir, nameOnly+"_ocr_res_img"+ext)
	visImageBytes, err := os.ReadFile(visImagePath)
	var visImageBase64 string
	if err == nil {
		// 若讀取成功，將圖片轉為 Base64 字串
		visImageBase64 = base64.StdEncoding.EncodeToString(visImageBytes)
	} else {
		// 若讀取失敗 (非致命錯誤)，僅打印 Warning，不中斷流程。
		fmt.Printf("Warning: reading visualization image failed: %v\n", err)
	}

	// 9. 回傳最終結果
	// 用途：回傳 JSON 回應，包含過濾後的文字與 Base64 圖片。
	return ctx.JSON(http.StatusOK, map[string]any{
		"filtered_texts": resultData["rec_filtered_texts"],
		"image_base64":   visImageBase64,
	})
}
