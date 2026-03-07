package agentrun

var allowedResultStatusValues = []string{
	"blocked",
	"completed",
	"completed_with_findings",
	"fail",
	"failed",
	"findings_detected",
	"needs_changes",
	"needs_fix",
	"pass",
	"pass_with_findings",
	"passed",
	"passed_with_warning",
	"success",
	"timeout",
}

var allowedResultStatusSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(allowedResultStatusValues))
	for _, status := range allowedResultStatusValues {
		set[status] = struct{}{}
	}
	return set
}()

func isAllowedResultStatus(status string) bool {
	_, ok := allowedResultStatusSet[status]
	return ok
}

func allowedResultStatuses() []string {
	values := make([]string, len(allowedResultStatusValues))
	copy(values, allowedResultStatusValues)
	return values
}
