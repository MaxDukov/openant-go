# Demo recording script for vhs (https://github.com/charmbracelet/vhs).
# Requires: vhs installed, libusb, and a real ANT USB stick attached.
#
#   vhs resources/demo.tape
#
# Output: resources/demo.gif — referenced from the README once recorded.

Output resources/demo.gif
Set Shell zsh
Set FontSize 20
Set Width 960
Set Height 540
Set Padding 24
Set Theme "Catppuccin Mocha"
Set CursorBlink false

Type "goant version"
Enter
Sleep 900ms

Type "goant sticks"
Enter
Sleep 1500ms

Type@200ms "goant scan -t HeartRate"
Enter
Sleep 6s

Ctrl+C
Sleep 700ms
