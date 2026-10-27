-- Bud.app runs `bud launch` (D46): on the dashboard when it's opened, or on
-- what was dropped on its icon. A droplet, because macOS hands dropped files
-- to an app as Apple Events, not arguments. bud bundle fills in which bud to
-- run and the PATH to run it with, since an app started from the Dock gets
-- only the system's
property budBin : "{{.Bin}}"
property budPath : "{{.Path}}"

on run
	launchBud({})
end run

on open droppedItems
	launchBud(droppedItems)
end open

on launchBud(droppedItems)
	set cmd to "cd ~ && PATH=" & quoted form of budPath & " exec " & quoted form of budBin & " launch"
	repeat with anItem in droppedItems
		set cmd to cmd & " " & quoted form of (POSIX path of anItem)
	end repeat
	try
		do shell script cmd
	on error errorMessage
		display alert "Bud couldn't open" message errorMessage as critical
	end try
end launchBud
