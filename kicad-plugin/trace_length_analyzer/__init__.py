"""PCB Trace Length Analyzer, as a KiCad plugin.

The analysis is the Go engine the website runs, started as a child process and
spoken to over its pipes. The report is the website's own front end, served into
a Qt window from a URL scheme answered in this process. Nothing listens on a
socket.
"""

__version__ = "0.1.4"
