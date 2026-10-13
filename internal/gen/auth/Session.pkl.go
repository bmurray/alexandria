// Code generated from Pkl module `alexandria.auth.Auth`. DO NOT EDIT.
package auth

type Session struct {
	CookieName string `pkl:"cookieName"`

	DurationHours uint16 `pkl:"durationHours"`

	// Set the Secure flag on the cookie. Must be true behind HTTPS.
	SecureCookie bool `pkl:"secureCookie"`
}
