elm make src/Main.elm --debug --output=assets/ui.js
uglifyjs assets/ui.js --output assets/ui.js
go build