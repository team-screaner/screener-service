package http

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/team-screaner/screener-service/api"
)

type workbookResponse struct{ data []byte }

// VisitExportXLSXResponse writes the generated contract binary response.
func (r workbookResponse) VisitExportXLSXResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="screener.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r.data)
	return err
}

// ExportXLSX returns a portable workbook using the same authorization as JSON reads.
func (s *server) ExportXLSX(ctx context.Context, _ api.ExportXLSXRequestObject) (api.ExportXLSXResponseObject, error) {
	result, err := s.execute(ctx, "exportXLSX", "")
	if err != nil {
		return nil, err
	}
	encoded, ok := result["content_base64"].(string)
	if !ok {
		return nil, errors.New("invalid workbook response")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return workbookResponse{data: data}, nil
}
