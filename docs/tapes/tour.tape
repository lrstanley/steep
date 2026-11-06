# The tea shop's tour, as the CLI pages show it. "task docs:demos" runs it with
# teashop on PATH, and exports the result with one flag changed at a time.
Set Shell "bash"
Set Columns 72
Set Rows 16
Set TypingSpeed 60ms
Set Title "tea shop"

Type "teashop" Enter
Wait+Screen "Sencha"
Sleep 400ms
Mark "browse:start"

Down Sleep 250ms
Down Sleep 250ms
Mark "filter:start"
Type "/green" Sleep 400ms
Mark "filter:typed"
Escape Sleep 250ms
Mark "filter:done"

Down Sleep 250ms
Mark "brew:start"
Enter
Wait+Screen "Matcha is ready"
Mark "brew:done"
# Long enough for the brew's desktop notification to show as a toast.
Sleep 2500ms
Mark "brew:toasted"
Type "q"
