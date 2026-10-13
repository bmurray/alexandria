// Code generated from Pkl module `alexandria.AppConfig`. DO NOT EDIT.
package appconfig

type Server struct {
	Host string `pkl:"host"`

	Port uint16 `pkl:"port"`

	// Origin the browser uses: OIDC redirect URIs and cookies derive from it.
	PublicUrl string `pkl:"publicUrl"`

	// Hostnames this instance answers on. The k8s schema derives Gateway
	// listeners and HTTPRoute hostnames from this list; the first is canonical.
	Hostnames []string `pkl:"hostnames"`

	// In development, proxy everything that is not /api to the Vite server.
	// Null serves the embedded web build.
	DevProxy *string `pkl:"devProxy"`

	ReadHeaderTimeoutSeconds uint16 `pkl:"readHeaderTimeoutSeconds"`

	ShutdownTimeoutSeconds uint16 `pkl:"shutdownTimeoutSeconds"`
}
