module github.com/odvcencio/gothub

go 1.25

require (
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/jackc/pgx/v5 v5.8.0
	github.com/odvcencio/got v0.1.0
	github.com/odvcencio/gotreesitter v0.2.0
	github.com/odvcencio/gts-suite v0.3.0
	golang.org/x/crypto v0.46.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.37.1
)

replace github.com/odvcencio/got => ../got
replace github.com/odvcencio/gotreesitter => ../gotreesitter
replace github.com/odvcencio/gts-suite => ../gts-suite

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	modernc.org/libc v1.65.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
