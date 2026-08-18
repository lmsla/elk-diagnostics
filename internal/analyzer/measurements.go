package analyzer

import "elk-diagnostics/internal/diagnostic"

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
