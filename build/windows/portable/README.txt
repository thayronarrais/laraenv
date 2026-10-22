LaraEnv - portable edition
==========================

Extract this folder anywhere and run LaraEnv.exe. Nothing is installed.

WHERE YOUR DATA LIVES
---------------------
Everything LaraEnv manages - config.json, your projects in www\, PHP, Nginx,
MySQL, and the rest - stays inside THIS folder. Move the folder, and your
whole environment moves with it. Delete it, and nothing is left behind
anywhere else on the machine.

What makes this work is the file named .laraenv-portable sitting next to
LaraEnv.exe. Do not delete it. Without it LaraEnv falls back to the normal
installed location (C:\laraenv) and this folder is ignored.

DO NOT EXTRACT INTO C:\LaraEnv
------------------------------
Windows does not distinguish upper from lower case, so a folder named
C:\LaraEnv is the same folder the normal installation uses. Extract somewhere
that cannot collide - C:\LaraEnv-Portable, D:\Dev\LaraEnv, or a USB stick.

WHAT STILL TOUCHES THE MACHINE
------------------------------
Portable means your data travels with you. A few things cannot be made
portable on Windows, and LaraEnv is honest about them:

  * Administrator rights are needed to write pretty .test domains into
    C:\Windows\System32\drivers\etc\hosts. Without them, reach your sites
    at localhost:<port> instead.
  * WebView2 must be present. Windows 11 ships it; Windows 10 may need it
    installed once.
  * Cron jobs that run with LaraEnv closed register Windows Task Scheduler
    entries, which are machine-wide by nature.

UPDATES
-------
LaraEnv updates itself in place: it downloads the new executable, verifies
its checksum, and replaces this one. The installer is never used for a
portable copy, so it can never create a second installation behind your back.

FIRST RUN
---------
The app opens empty - no services, no projects. That is expected. Install
what you need from the interface and it lands inside this folder.
