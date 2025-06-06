package ai

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

type IImageToTextPresenter interface {
	PaddXServi(ctx echo.Context) error
}
type imageRequest struct {
	Photo []byte `json:"Photo"`
}

func NewImageToText() IImageToTextPresenter {
	return &imageRequest{}
}

// @Summary AI 圖片Servi轉文字
// @description 圖片Servi轉文字
// @Tags ai 圖片轉文字
// @version 1.0
// @Accept json multipart/form-data
// @produce json
// @param file formData file true "要上傳的圖片"
// @Success 200 {object} map[string]interface{} "成功時回傳過濾後的 rec_texts 陣列"
// @Failure 400 {object} map[string]string "無法取得圖片"
// @Failure 500 {object} map[string]string "內部錯誤，例如圖片儲存錯誤、執行 CLI 錯誤或無法讀取結果檔案"
// @Router /api/ai/image/orc/text [post]
func (p *imageRequest) PaddXServi(ctx echo.Context) error {
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

	// 修改這裡：input/output 路徑
	uploadDir := "C:\\Users\\jo87j\\Desktop\\paddx_input\\"
	outputDir := "C:\\Users\\jo87j\\Desktop\\paddx_output\\"

	// 確保資料夾存在
	os.MkdirAll(uploadDir, os.ModePerm)
	os.MkdirAll(outputDir, os.ModePerm)

	// 用原始檔名儲存圖片
	inputPath := filepath.Join(uploadDir, file.Filename)

	dst, err := os.Create(inputPath)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法儲存圖片"})
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "儲存圖片失敗"})
	}

	// 3. 呼叫 PaddX CLI
	cmd := exec.Command("paddlex",
		"--pipeline", "OCR",
		"--input", inputPath,
		"--use_doc_orientation_classify", "False",
		"--use_doc_unwarping", "False",
		"--use_textline_orientation", "False",
		"--save_path", outputDir,
		"--device", "gpu",
	)

	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error":   "paddx 執行錯誤",
			"details": string(cmdOutput),
		})
	}

	// 4. 讀取 PaddX 的輸出結果
	ext := filepath.Ext(file.Filename)                 // 取得副檔名，例如 ".png"
	nameOnly := strings.TrimSuffix(file.Filename, ext) // 去除副檔名
	resultFile := filepath.Join(outputDir, nameOnly+"_res.json")
	resultBytes, err := os.ReadFile(resultFile)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "無法讀取結果 JSON"})
	}

	// 解析回來，然後直接當成物件回傳
	var resultData map[string]interface{}
	// resultBytes 是原本就已經是 json.Marshal 出來的 []byte
	err = json.Unmarshal(resultBytes, &resultData)

	// 過濾掉 rec_scores < 0.85 的 rec_texts
	if scores, ok := resultData["rec_scores"].([]interface{}); ok {
		if texts, ok := resultData["rec_texts"].([]interface{}); ok {
			var filteredTexts []string
			for i, s := range scores {
				if scoreFloat, ok := s.(float64); ok && scoreFloat >= 0.85 {
					if i < len(texts) {
						if textStr, ok := texts[i].(string); ok {
							filteredTexts = append(filteredTexts, textStr)
						}
					}
				}
			}
			resultData["rec_filtered_texts"] = filteredTexts
		}
	}
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to parse resultBytes",
		})
	}

	// 假設輸出的圖片為 *_res.png
	visImagePath := filepath.Join(outputDir, nameOnly+"_ocr_res_img"+ext)
	visImageBytes, err := os.ReadFile(visImagePath)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "無法讀取定位後圖片",
		})
	}

	// 將圖片轉為 base64
	visImageBase64 := base64.StdEncoding.EncodeToString(visImageBytes)
	// 給全資料
	// return ctx.JSON(http.StatusOK, resultData)
	// 只給filtered後的資料
	// return ctx.JSON(http.StatusOK, resultData["rec_filtered_texts"])
		// 回傳 json 包含文字 + base64 圖片
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"filtered_texts": resultData["rec_filtered_texts"],
		"image_base64":   visImageBase64,
	})
}
