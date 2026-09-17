package analyzer

import "elk-diagnostics/internal/diagnostic"

func numericStatus(value float64, warningAt, criticalAt int) diagnostic.Status {
	switch {
	case criticalAt > 0 && value >= float64(criticalAt):
		return diagnostic.StatusCritical
	case warningAt > 0 && value >= float64(warningAt):
		return diagnostic.StatusWarning
	default:
		return diagnostic.StatusPass
	}
}

func numericRow(entity string, current, limit, ratio *float64, status diagnostic.Status) diagnostic.NumericJudgmentRow {
	return diagnostic.NumericJudgmentRow{Entity: entity, Current: current, Limit: limit, Ratio: ratio, Status: status}
}

func float64p(value float64) *float64 {
	return &value
}

func numericJudgment(metric, label, valueUnit string, warningAt, criticalAt int, source, note string) diagnostic.NumericJudgment {
	judgment := diagnostic.NumericJudgment{
		Metric:          metric,
		MetricLabel:     label,
		CurrentLabel:    "已用",
		LimitLabel:      "上限",
		RatioLabel:      "壓力",
		ValueUnit:       valueUnit,
		RatioUnit:       "percent",
		ThresholdSource: source,
		SnapshotNote:    note,
	}
	if warningAt > 0 {
		judgment.WarningAt = float64p(float64(warningAt))
	}
	if criticalAt > 0 {
		judgment.CriticalAt = float64p(float64(criticalAt))
	}
	return judgment
}

func gauge(metric string, value float64, unit, entityType, entityID, entityName, component string) diagnostic.Measurement {
	return diagnostic.Measurement{Metric: metric, Kind: "gauge", Value: value, Unit: unit, EntityType: entityType, EntityID: entityID, EntityName: entityName, Component: component}
}

func gaugeInPeerGroup(metric string, value float64, unit, entityType, entityID, entityName, component, peerGroup string) diagnostic.Measurement {
	m := gauge(metric, value, unit, entityType, entityID, entityName, component)
	m.PeerGroup = peerGroup
	return m
}

func counter(metric string, value float64, unit, entityType, entityID, entityName, component string) diagnostic.Measurement {
	return diagnostic.Measurement{Metric: metric, Kind: "counter", Value: value, Unit: unit, EntityType: entityType, EntityID: entityID, EntityName: entityName, Component: component}
}
