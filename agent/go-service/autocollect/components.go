package autocollect

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type historyParam struct {
	Route string `json:"route"`
	Event string `json:"event,omitempty"`
}

var routeName = regexp.MustCompile(`^AutoCollect(?:Common)?Route[1-9][0-9]*$`)

func accountIdentity(ctx *maa.Context) string {
	raw, err := ctx.GetNodeJSON("CurrentAccountIdentity")
	if err != nil {
		return ""
	}
	var node struct {
		Attach struct {
			AccountID string `json:"account_id"`
		} `json:"attach"`
	}
	if json.Unmarshal([]byte(raw), &node) != nil {
		return ""
	}
	return node.Attach.AccountID
}

// HistoryAction records a route attempt or a verified route-end completion.
type HistoryAction struct{}

var _ maa.CustomActionRunner = &HistoryAction{}

func (a *HistoryAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var param historyParam
	if json.Unmarshal([]byte(arg.CustomActionParam), &param) != nil || !routeName.MatchString(param.Route) || (param.Event != "started" && param.Event != "completed") {
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil || tasker.Stopping() {
		return false
	}
	account := accountIdentity(ctx)
	if account == "" {
		log.Warn().Str("component", "AutoCollectHistory").Str("route", param.Route).Msg("account unknown; cooldown history is disabled")
		return true
	}
	if err := history.record(account, param.Route, param.Event, time.Now()); err != nil {
		log.Error().Err(err).Str("component", "AutoCollectHistory").Str("route", param.Route).Msg("failed to save route history")
		return false
	}
	log.Info().Str("component", "AutoCollectHistory").Str("account_id", account).Str("route", param.Route).Str("event", param.Event).Msg("route history saved")
	return true
}

// RouteDueRecognition allows regular routes without a completion since the local 04:00 reset.
type RouteDueRecognition struct {
	notices routeNotices
}

var _ maa.CustomRecognitionRunner = &RouteDueRecognition{}

func (r *RouteDueRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		return nil, false
	}
	var param historyParam
	if json.Unmarshal([]byte(arg.CustomRecognitionParam), &param) != nil || !routeName.MatchString(param.Route) {
		return nil, false
	}
	account := accountIdentity(ctx)
	value, err := history.lookup(account, param.Route)
	if err != nil {
		log.Warn().Err(err).Str("component", "AutoCollectHistory").Str("route", param.Route).Msg("cannot read cooldown; route remains due")
	}
	now := time.Now()
	if account != "" && err == nil && collectedToday(value.LastSuccess, now) {
		nextReset := resetBoundary(now).AddDate(0, 0, 1)
		log.Info().Str("component", "AutoCollectHistory").Str("route", param.Route).Time("last_attempt", value.LastAttempt).Time("last_success", value.LastSuccess).Time("next_reset", nextReset).Msg("skipping route already collected this game day")
		if r.notices.first(arg.TaskID, account+":"+param.Route+":cooldown") {
			maafocus.Print(ctx, i18n.T("autocollect.cooldown_skip", i18n.T("option."+param.Route+".label"), value.LastSuccess.Local().Format(time.RFC3339), nextReset.Format(time.RFC3339)))
		}
		return nil, false
	}
	log.Info().Str("component", "AutoCollectHistory").Str("route", param.Route).Time("last_attempt", value.LastAttempt).Time("last_success", value.LastSuccess).Msg("route is due")
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: fmt.Sprintf(`{"route":%q,"due":true}`, param.Route)}, true
}
