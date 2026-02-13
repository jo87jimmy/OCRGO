package ai // 定義 ai 套件，負責處理與 AI 相關的邏輯

import ( // 匯入所需的標準函式庫與外部套件
	"encoding/base64" // 用於將圖片資料編碼為 Base64 字串，以便在 JSON 中傳輸
	"encoding/json"   // 用於處理 JSON 資料的編碼與解碼
	"io"              // 提供基本的 I/O 介面，例如複製檔案內容
	"net/http"        // 提供 HTTP 客戶端與伺服器實作，這裡用於定義 HTTP 狀態碼
	"os"              // 提供作業系統功能的介面，例如檔案操作與目錄建立
	"os/exec"         // 用於執行外部命令，這裡用來呼叫 PaddX CLI
	"path/filepath"   // 用於處理檔案路徑，確保跨平台相容性
	"strings"         // 提供字串處理功能，例如去除副檔名

	"github.com/labstack/echo/v4" // 匯入 Echo Web 框架，用於處理 HTTP 請求與回應
)

// ImageToTextPresenter 定義 OCR 圖片轉文字 Presenter 的介面 (Basic/V1)
type ImageToTextPresenter interface { // 定義介面，規範圖片轉文字的功能
	ExtractText(ctx echo.Context) error // ExtractText 方法，接收 Echo Context 並回傳錯誤，負責處理請求
}

// imageToTextPresenter 實作 ImageToTextPresenter 介面
type imageToTextPresenter struct { // 定義結構體，實作 ImageToTextPresenter 介面
	Photo []byte `json:"Photo"` // Photo 欄位，用於接收或儲存圖片的 byte 資料，對應 JSON 欄位 "Photo"
}

// NewImageToTextPresenter 建立 ImageToTextPresenter 的實例
func NewImageToTextPresenter() ImageToTextPresenter { // 建構函式，回傳 ImageToTextPresenter 介面實例
	return &imageToTextPresenter{} // 回傳 imageToTextPresenter 的實例指標
}

// ExtractText 執行圖片轉文字 (PaddX)
// @Summary AI 圖片轉文字
// @description 圖片轉文字
// @Tags ai 圖片轉文字
// @version 1.0
// @Accept json multipart/form-data
// @produce json
// @param file formData file true "要上傳的圖片"
// @Success 200 {object} map[string]interface{} "成功時回傳過濾後的 rec_texts 陣列"
// @Failure 400 {object} map[string]string "無法取得圖片"
// @Failure 500 {object} map[string]string "內部錯誤"
// @Router /api/ai/image/orc/text [post]
func (p *imageToTextPresenter) ExtractText(ctx echo.Context) error { // 實作 ExtractText 方法，處理 HTTP 請求
	// 1. 取得圖片
	file, err := ctx.FormFile("file") // 從請求上下文獲取名為 "file" 的上傳檔案
	if err != nil {                   // 如果獲取檔案發生錯誤
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "無法取得圖片"}) // 回傳 400 錯誤與錯誤訊息
	}

	src, err := file.Open() // 打開上傳的檔案
	if err != nil {         // 如果打開檔案發生錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法打開圖片檔案"}) // 回傳 500 錯誤與錯誤訊息
	}
	defer src.Close() // 確保函式結束時關閉檔案，釋放資源

	// 蔡- 修改這裡：input/output 路徑
	// 注意：硬編碼路徑在跨環境時容易出錯，建議透過 Config 注入
	uploadDir := "C:\\Users\\jo87j\\Desktop\\paddx_input\\"  // 定義上傳圖片的暫存目錄路徑
	outputDir := "C:\\Users\\jo87j\\Desktop\\paddx_output\\" // 定義 PaddX 輸出的目錄路徑

	// 確保資料夾存在
	os.MkdirAll(uploadDir, os.ModePerm) // 建立上傳目錄，若已存在則忽略，權限設為 ModePerm
	os.MkdirAll(outputDir, os.ModePerm) // 建立輸出目錄，若已存在則忽略，權限設為 ModePerm

	// 用原始檔名儲存圖片
	inputPath := filepath.Join(uploadDir, file.Filename) // 組合完整的輸入檔案路徑

	dst, err := os.Create(inputPath) // 建立目標檔案
	if err != nil {                  // 如果建立檔案發生錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法儲存圖片"}) // 回傳 500 錯誤與錯誤訊息
	}
	defer dst.Close() // 確保函式結束時關閉目標檔案

	if _, err := io.Copy(dst, src); err != nil { // 將上傳的檔案內容複製到目標檔案
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "儲存圖片失敗"}) // 若複製失敗，回傳 500 錯誤
	}

	// 3. 呼叫 PaddX CLI
	cmd := exec.Command("paddlex", // 建立外部指令，執行 paddlex
		"--pipeline", "OCR", // 指定 pipeline 為 OCR
		"--input", inputPath, // 指定輸入圖片路徑
		"--use_doc_orientation_classify", "False", // 停用文件方向分類功能
		"--use_doc_unwarping", "False", // 停用文件校正功能
		"--use_textline_orientation", "False", // 停用文字行方向檢測
		"--save_path", outputDir, // 指定輸出結果儲存路徑
		"--device", "gpu", // 指定使用 GPU 進行運算
	)

	cmdOutput, err := cmd.CombinedOutput() // 執行指令並獲取標準輸出與標準錯誤輸出
	if err != nil {                        // 如果執行指令發生錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{ // 回傳 500 錯誤
			"error":   "paddx 執行錯誤",      // 錯誤訊息：paddx 執行錯誤
			"details": string(cmdOutput), // 包含詳細的指令輸出內容以便除錯
		})
	}

	// 4. 讀取 PaddX 的輸出結果
	ext := filepath.Ext(file.Filename)                           // 取得上傳檔案的副檔名，例如 ".png"
	nameOnly := strings.TrimSuffix(file.Filename, ext)           // 去除副檔名，取得檔名主體
	resultFile := filepath.Join(outputDir, nameOnly+"_res.json") // 組合結果 JSON 檔案的路徑
	resultBytes, err := os.ReadFile(resultFile)                  // 讀取結果 JSON 檔案的內容
	if err != nil {                                              // 如果讀取檔案發生錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法讀取結果 JSON"}) // 回傳 500 錯誤
	}

	// 解析結果
	var resultData map[string]any // 定義變數儲存解析後的 JSON 資料，使用 interface{} 以容納任意結構
	// resultBytes 是原本就已經是 json.Marshal 出來的 []byte
	err = json.Unmarshal(resultBytes, &resultData) // 將讀取的 JSON bytes 解析到 resultData map 中

	// 過濾掉 rec_scores < 0.85 的 rec_texts
	if scores, ok := resultData["rec_scores"].([]any); ok { // 嘗試取得 rec_scores 欄位並轉型為 slice
		if texts, ok := resultData["rec_texts"].([]any); ok { // 嘗試取得 rec_texts 欄位並轉型為 slice
			var filteredTexts []string // 定義用於儲存過濾後文字的切片
			for i, s := range scores { // 遍歷分數列表
				if scoreFloat, ok := s.(float64); ok && scoreFloat >= 0.85 { // 檢查分數是否為 float64 且大於等於 0.85
					if i < len(texts) { // 確保索引在文字列表範圍內
						if textStr, ok := texts[i].(string); ok { // 嘗試將對應的文字轉為字串
							filteredTexts = append(filteredTexts, textStr) // 將符合條件的文字加入過濾列表
						}
					}
				}
			}
			resultData["rec_filtered_texts"] = filteredTexts // 將過濾後的文字列表存回 resultData
		}
	}
	if err != nil { // 檢查 JSON 解析或其他錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{ // 回傳 500 錯誤
			"error": "failed to parse resultBytes", // 錯誤訊息：解析 JSON 失敗
		})
	}

	// 假設輸出的圖片為 *_res.png
	visImagePath := filepath.Join(outputDir, nameOnly+"_ocr_res_img"+ext) // 組合 OCR 結果圖片的路徑 (注意：這裡假設輸出檔名後綴為 _ocr_res_img)
	visImageBytes, err := os.ReadFile(visImagePath)                       // 讀取結果圖片的內容
	if err != nil {                                                       // 如果讀取圖片發生錯誤
		return ctx.JSON(http.StatusInternalServerError, map[string]string{ // 回傳 500 錯誤
			"error": "無法讀取定位後圖片", // 錯誤訊息：無法讀取定位後圖片
		})
	}

	// 將圖片轉為 base64
	visImageBase64 := base64.StdEncoding.EncodeToString(visImageBytes) // 將圖片 bytes 編碼為 Base64 字串

	// 回傳 json 包含文字 + base64 圖片
	return ctx.JSON(http.StatusOK, map[string]any{
		"filtered_texts": resultData["rec_filtered_texts"], // 回傳過濾後的文字列表
		"image_base64":   visImageBase64,                   // 回傳 Base64 編碼的結果圖片
	})
}
