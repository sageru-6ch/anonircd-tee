# AnonIRCd

AnonIRCd is an anonymous IRC daemon.  All messages appear to be written by **Anonymous**.

#### Try AnonIRCd by joining AnonIRC

Connect to [**z.1chan.us:6667**](irc://z.1chan.us:6667) or [**:6697 (SSL)**](ircs://z.1chan.us:6697).

All new clients auto-join a channel named `#`.  `/list` to see all non-secret channels.  `/join #anonirc` if you'd like to discuss the daemon.

## Modes

Mode | Type | Description
--- | --- | ---
c | User & Channel | Hide user count (always set to 1)
D | User & Channel | Delay user count updates (joins/parts) until someone speaks
k *key* | Channel | Set channel key (password) required to join
l *limit* | Channel | Set user limit
