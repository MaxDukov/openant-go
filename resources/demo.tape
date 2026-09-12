# Demo recording script for vhs (https://github.com/charmbracelet/vhs).
# Requires: vhs installed, libusb, and a real ANT USB stick attached
# (plus an ANT+ heart rate sensor in range for the scan part).
#
#   vhs resources/demo.tape
#
# Output: resources/demo.gif — embedded in the README.
# The "pi" host alias points to the Raspberry Pi with the sticks.

Output "resources/demo.gif"
Set Shell zsh
Set FontSize 18
Set Width 960
Set Height 540
Set Padding 20
Set Theme "Catppuccin Mocha"
Set CursorBlink false
Set TypingSpeed 40ms

Type "ssh pi"
Enter
Sleep 2s

Type "goant version"
Enter
Sleep 800ms

Type "goant sticks"
Enter
Sleep 2s

Type "goant scan -serials 1:7 -t HeartRate -auto_create"
Enter
Sleep 12s

Ctrl+C
Sleep 600ms

Type "exit"
Enter
Sleep 1s
