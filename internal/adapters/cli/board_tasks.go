package cli

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"

	"rhizome-mcp/internal/domain"
)

type boardIssueLister interface {
	ListIssues(context.Context, domain.ListIssuesInput) (domain.IssueList, error)
}

type taskPageLink struct {
	Label   string
	Href    string
	Current bool
}

type taskPageRow struct {
	ID, Title, Href, Status, StatusLabel, Type, Priority string
	Labels                                               []string
}

type taskPageColumn struct {
	Status domain.EffectiveStatus
	Label  string
	Rows   []taskPageRow
}

type taskPageViewModel struct {
	Style       template.CSS
	IsBoard     bool
	Modes       []taskPageLink
	Filters     []taskPageLink
	Rows        []taskPageRow
	Columns     []taskPageColumn
	RefreshHref string
	FirstHref   string
	NextHref    string
}

func serveTaskPage(w http.ResponseWriter, request *http.Request, service BoardHTTPService) {
	lister, ok := service.(boardIssueLister)
	if !ok {
		http.Error(w, "Task views unavailable", http.StatusServiceUnavailable)
		return
	}
	query := request.URL.Query()
	mode := query.Get("view")
	status := domain.EffectiveStatus(query.Get("status"))
	cursor := query.Get("cursor")
	if (mode != "" && mode != "list" && mode != "board") || (status != "" && !status.Valid()) {
		http.Error(w, "Invalid task view or status", http.StatusBadRequest)
		return
	}
	if mode == "" {
		mode = "list"
	}
	input := domain.ListIssuesInput{Limit: domain.MaxCollectionLimit, Cursor: cursor}
	if status != "" {
		input.EffectiveStatuses = []domain.EffectiveStatus{status}
	}
	page, err := lister.ListIssues(request.Context(), input)
	if err != nil {
		var domainErr *domain.Error
		if errors.As(err, &domainErr) && domainErr.Code == domain.CodeInvalidArgument {
			http.Error(w, domainErr.Message, http.StatusBadRequest)
			return
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	body, err := renderBoardTemplate("taskPage", newTaskPageViewModel(page, mode, status, cursor))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeBoardHTTPResponse(w, http.StatusOK, "text/html; charset=utf-8", []byte(body), request.Method != http.MethodHead)
}

func newTaskPageViewModel(page domain.IssueList, mode string, status domain.EffectiveStatus, cursor string) taskPageViewModel {
	columns := []taskPageColumn{
		{Status: domain.EffectiveStatusOpen, Label: "Open"},
		{Status: domain.EffectiveStatusReady, Label: "Ready"},
		{Status: domain.EffectiveStatusInProgress, Label: "In progress"},
		{Status: domain.EffectiveStatusBlocked, Label: "Blocked"},
		{Status: domain.EffectiveStatusReview, Label: "In review"},
		{Status: domain.EffectiveStatusDone, Label: "Done"},
		{Status: domain.EffectiveStatusCancelled, Label: "Cancelled"},
	}
	current := taskPageQuery(mode, status, cursor)
	vm := taskPageViewModel{
		Style:       template.CSS(taskHTMLStyle),
		IsBoard:     mode == "board",
		RefreshHref: "/tasks?" + current,
		Modes: []taskPageLink{
			{"List", "/tasks?" + taskPageQuery("list", status, cursor), mode == "list"},
			{"Board", "/tasks?" + taskPageQuery("board", status, cursor), mode == "board"},
		},
		Filters: []taskPageLink{{"All", "/tasks?" + taskPageQuery(mode, "", ""), status == ""}},
	}
	columnIndices := make(map[domain.EffectiveStatus]int, len(columns))
	for index, column := range columns {
		columnIndices[column.Status] = index
		vm.Filters = append(vm.Filters, taskPageLink{
			column.Label, "/tasks?" + taskPageQuery(mode, column.Status, ""), status == column.Status,
		})
	}
	for _, issue := range page.Items {
		column := &columns[columnIndices[issue.EffectiveStatus]]
		id := issueDisplayIDForProjection(issue, nil)
		if id == "" {
			id = issue.ID
		}
		row := taskPageRow{
			ID: id, Title: issue.Title, Href: boardIssuePath(issue.ID, id) + "?" + current,
			Status: string(issue.EffectiveStatus), StatusLabel: column.Label,
			Type: string(issue.Type), Priority: string(issue.Priority),
		}
		for _, label := range issue.Labels {
			row.Labels = append(row.Labels, label.Name)
		}
		vm.Rows = append(vm.Rows, row)
		column.Rows = append(column.Rows, row)
	}
	vm.Columns = columns
	if status != "" {
		vm.Columns = []taskPageColumn{columns[columnIndices[status]]}
	}
	if cursor != "" {
		vm.FirstHref = "/tasks?" + taskPageQuery(mode, status, "")
	}
	if page.HasMore && page.NextCursor != nil {
		vm.NextHref = "/tasks?" + taskPageQuery(mode, status, *page.NextCursor)
	}
	return vm
}

func taskPageQuery(mode string, status domain.EffectiveStatus, cursor string) string {
	query := url.Values{"view": {mode}}
	if status != "" {
		query.Set("status", string(status))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	return query.Encode()
}

func taskReturnQuery(query url.Values) string {
	mode := query.Get("view")
	if mode != "list" && mode != "board" {
		return ""
	}
	return taskPageQuery(mode, domain.EffectiveStatus(query.Get("status")), query.Get("cursor"))
}
