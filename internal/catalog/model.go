package catalog

import (
	"fmt"
	"strings"
	"time"
)

type HubKind string

const (
	HubRoad       HubKind = "road"
	HubRail       HubKind = "rail"
	HubPort       HubKind = "port"
	HubAir        HubKind = "air"
	HubPostal     HubKind = "postal"
	HubMultimodal HubKind = "multimodal"
)

type AssetKind string

const (
	AssetDeliveryVehicle AssetKind = "delivery_vehicle"
	AssetInspectionDrone AssetKind = "inspection_drone"
	AssetSortingRobot    AssetKind = "sorting_robot"
	AssetHeavyTruck      AssetKind = "heavy_truck"
	AssetRailSensor      AssetKind = "rail_sensor"
	AssetPortCrane       AssetKind = "port_crane"
)

type Hub struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Kind      HubKind   `json:"kind"`
	Status    string    `json:"status"`
	Capacity  int       `json:"capacity"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Asset struct {
	ID           string    `json:"id"`
	AssetNo      string    `json:"asset_no"`
	Kind         AssetKind `json:"kind"`
	HubID        string    `json:"hub_id"`
	Status       string    `json:"status"`
	Capabilities []string  `json:"capabilities"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateHubRequest struct {
	Code     string
	Name     string
	Kind     HubKind
	Capacity int
	ActorID  string
}

type CreateAssetRequest struct {
	AssetNo      string
	Kind         AssetKind
	HubID        string
	Capabilities []string
	ActorID      string
}

type AssetFilter struct {
	HubID string
	Kind  AssetKind
	State string
	Limit int
	Page  int
}

func (r CreateHubRequest) Validate() error {
	if strings.TrimSpace(r.Code) == "" || strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("hub code and name are required")
	}
	if !validHubKind(r.Kind) {
		return fmt.Errorf("unsupported hub kind %q", r.Kind)
	}
	if r.Capacity < 1 {
		return fmt.Errorf("hub capacity must be positive")
	}
	if strings.TrimSpace(r.ActorID) == "" {
		return fmt.Errorf("actor is required")
	}
	return nil
}

func (r CreateAssetRequest) Validate() error {
	if strings.TrimSpace(r.AssetNo) == "" || strings.TrimSpace(r.HubID) == "" {
		return fmt.Errorf("asset number and hub are required")
	}
	if !validAssetKind(r.Kind) {
		return fmt.Errorf("unsupported asset kind %q", r.Kind)
	}
	if strings.TrimSpace(r.ActorID) == "" {
		return fmt.Errorf("actor is required")
	}
	seen := make(map[string]struct{}, len(r.Capabilities))
	for _, capability := range r.Capabilities {
		value := strings.TrimSpace(capability)
		if value == "" {
			return fmt.Errorf("capability cannot be empty")
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("capability %q is duplicated", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (f AssetFilter) Normalize() AssetFilter {
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	return f
}

func validHubKind(kind HubKind) bool {
	switch kind {
	case HubRoad, HubRail, HubPort, HubAir, HubPostal, HubMultimodal:
		return true
	default:
		return false
	}
}

func validAssetKind(kind AssetKind) bool {
	switch kind {
	case AssetDeliveryVehicle, AssetInspectionDrone, AssetSortingRobot, AssetHeavyTruck, AssetRailSensor, AssetPortCrane:
		return true
	default:
		return false
	}
}
