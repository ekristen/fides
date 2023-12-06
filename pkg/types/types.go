package types

type ClusterPutRequest struct {
	ID        string              `path:"id"`
	UID       string              `json:"uid"`
	JWKS      JWKS                `json:"jwks"`
	OIDConfig OpenIDConfiguration `json:"oid_config"`
}

type OpenIDConfiguration struct {
	Issuer                           string   `json:"issuer"`
	JwksUri                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Use string `json:"use"`
	Kty string `json:"kty"`
	Kid string `json:"kid" gorm:"uniqueIndex:idx_cluster_key"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type MetadataResponse struct {
	Count int `json:"count"`
}

type DataResponse struct {
	Success bool         `json:"success,omitempty"`
	Data    *interface{} `json:"data,omitempty"`
}

type Response struct {
	ErrorResponse
	DataResponse

	Metadata *MetadataResponse `json:"metadata,omitempty"`
}

type ClusterNewRequest struct {
	Name      string              `json:"name"`
	UID       string              `json:"uid"`
	JWKS      JWKS                `json:"jwks"`
	OIDConfig OpenIDConfiguration `json:"oid_config"`
}

type ClusterNewResponse struct {
	Token string `json:"token"`
	UID   string `json:"uid"`
	URL   string `json:"url"`
	Name  string `json:"name"`
}
