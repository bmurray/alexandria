// Code generated from Pkl module `alexandria.AppConfig`. DO NOT EDIT.
package appconfig

type Library struct {
	// Shown in the header and the page title.
	Name string `pkl:"name"`

	// Loan period.
	LoanDays uint16 `pkl:"loanDays"`

	// Days a ready hold waits for pickup before it lapses to the next person.
	HoldPickupDays uint16 `pkl:"holdPickupDays"`

	// Seed the catalog with a starter set of classics when the books table is empty.
	SeedOnEmpty bool `pkl:"seedOnEmpty"`
}
