package provider

import (
	"context"

	"smallx/internal/model"
)

type Provider interface {
	Name() string
	FetchNode(ctx context.Context) (node model.NodeConfig, changed bool, err error)
	FetchUsers(ctx context.Context) (users []model.UserInfo, changed bool, err error)
	FetchRules(ctx context.Context) (rules []model.AuditRule, changed bool, err error)
	ReportTraffic(ctx context.Context, traffic []model.TrafficReport) error
	ReportAliveIPs(ctx context.Context, alive []model.AliveIP) error
	ReportStatus(ctx context.Context, status model.StatusReport) error
	ReportAudits(ctx context.Context, audits []model.AuditLog) error
}
