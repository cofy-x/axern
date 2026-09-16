Output apps/docs/public/terminal/axern.gif

Set Shell "bash"
Set FontSize 18
Set Width 960
Set Height 600
Set Padding 24
Set TypingSpeed 18ms
Env HOME "~"
Type "axern --help | grep -E '^  (local|run|ssh|tunnel) '"
Enter
Sleep 4s
Ctrl+L
Type "axern run --help | sed -n '1,11p'"
Enter
Sleep 5s
