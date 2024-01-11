package loki

import (
	"github.com/grafana/loki/pkg/push"
	"github.com/prometheus/common/model"
)

type Entry struct {
	push.Entry
	Labels model.LabelSet
}
