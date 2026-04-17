package adapters

import (
	"log/slog"
	"murl/internal/model/auditlog"
	"net/url"
)

const (
	AuditFileID = "AuditFile"
	AuditUrlID  = "AuditURL"
)

type AuditlogConfig interface {
	AuditFile() string
	AuditURL() *url.URL
}

type AuditlogSubscriber interface {
	Register(s auditlog.Subscriber)
	UnRegister(s auditlog.Subscriber)
}

func AddAuditConsumers(cfg AuditlogConfig, a AuditlogSubscriber) {
	if af := cfg.AuditFile(); af != "" {
		a.Register(NewFileAuditlog(AuditFileID, af))
		slog.Info("register file audit consumer",
			slog.String("audit_file", af),
		)
	}
	if au := cfg.AuditURL(); au != nil {
		a.Register(NewUrlAuditlog(AuditUrlID, au.String()))
		slog.Info("register URL audit consumer",
			slog.String("audit_url", au.String()),
		)
	}
}
