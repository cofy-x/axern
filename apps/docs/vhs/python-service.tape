Output apps/docs/public/terminal/python-service.gif

Set Shell "bash"
Set FontSize 23
Set Width 960
Set Height 380
Set Padding 24
Set TypingSpeed 12ms
Set WaitTimeout 180s
Hide
Type "uv run --package axern-sdk python apps/docs/scripts/record-service-demo.py"
Enter
Wait+Screen /recording-ready/
Enter
Sleep 250ms
Show
Wait+Screen /cleanup=complete/
Sleep 10s
