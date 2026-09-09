package webui

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/brightpuddle/clara/internal/supervisor"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleApprovalsList renders pending HITL approval requests.
func (w *WebUI) handleApprovalsList(c echo.Context) error {
	var approvals []supervisor.ApprovalRequest
	if w.approvals != nil {
		approvals = w.approvals.List()
	}

	vm := &ui.ApprovalsVM{
		Base:      w.baseVM("Approvals", "/ui/approvals"),
		Approvals: approvals,
	}

	return render(c, http.StatusOK, ui.Approvals(vm))
}

// handleApprovalDecide processes a human decision for a pending approval request.
func (w *WebUI) handleApprovalDecide(c echo.Context) error {
	id := c.Param("id")
	optionStr := c.FormValue("option")
	optionNum, err := strconv.Atoi(optionStr)
	if err != nil || optionNum <= 0 {
		return w.renderApprovalsWithFlash(c, "error", "Invalid option selected")
	}

	if w.approvals == nil {
		return w.renderApprovalsWithFlash(c, "error", "Approval store unavailable")
	}

	if err := w.approvals.Decide(id, optionNum); err != nil {
		return w.renderApprovalsWithFlash(c, "error", fmt.Sprintf("Failed to record decision: %v", err))
	}

	return w.renderApprovalsWithFlash(c, "success", fmt.Sprintf("Decision recorded for request %s", id))
}

func (w *WebUI) renderApprovalsWithFlash(c echo.Context, flashKind, flash string) error {
	var approvals []supervisor.ApprovalRequest
	if w.approvals != nil {
		approvals = w.approvals.List()
	}

	vm := &ui.ApprovalsVM{
		Base:      w.baseVM("Approvals", "/ui/approvals"),
		Approvals: approvals,
		Flash:     flash,
		FlashKind: flashKind,
	}

	return render(c, http.StatusOK, ui.Approvals(vm))
}
