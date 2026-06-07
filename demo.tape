# pixeltui demo — render with:  make demo   (or: vhs demo.tape)
# Requires: vhs + ttyd + ffmpeg.  Output: docs/demo.gif

Output docs/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1180
Set Height 700
Set Padding 18
Set Theme "Catppuccin Mocha"

# Launch into the search-first screen.
Type "./pixeltui" Enter
Sleep 2.5s

# Search YouTube Music.
Type "daft punk" Sleep 600ms Enter
Sleep 4s

# Scroll the results.
Down Sleep 400ms
Down Sleep 400ms
Down Sleep 600ms

# Queue a couple and play.
Type "a" Sleep 500ms
Down Sleep 300ms
Type "a" Sleep 600ms
Enter
Sleep 6s

# Show the per-track actions menu.
Type "." Sleep 2s
Escape Sleep 500ms

# Open the browse menu (Liked / playlists / sources).
Type "b" Sleep 2.5s
Escape Sleep 500ms

# Show the keyboard help page (rendered as tables).
Type "?" Sleep 3s
Escape Sleep 500ms

# Quit.
Type "q"
Sleep 1s
