package output

type ServiceIDsResultJSON struct {
	ServiceIDs []string `json:"service_ids"`
}

type ServicePurgeResponseJSON struct {
	ServiceID string `json:"service_id"`
}

type ServiceDeleteResponseJSON struct {
	ServiceID string `json:"service_id"`
	Purged    bool   `json:"purged"`
}

func NewServiceIDsResultJSON(serviceIDs []string) ServiceIDsResultJSON {
	return ServiceIDsResultJSON{ServiceIDs: append([]string(nil), serviceIDs...)}
}

func NewServiceDeleteResponseJSON(serviceID string, purged bool) ServiceDeleteResponseJSON {
	return ServiceDeleteResponseJSON{ServiceID: serviceID, Purged: purged}
}

func NewServicePurgeResponseJSON(serviceID string) ServicePurgeResponseJSON {
	return ServicePurgeResponseJSON{ServiceID: serviceID}
}
