AnonIRCd
--------

##### Try it out:

Connect to [z.1chan.us:6667](irc://z.1chan.us:6667) or [:6697 (SSL)](ircs://z.1chan.us:6697)

##### TODO:
- database (sqlite for portability?)
- admin/mod login via server password
- admin/mod commands via /anonirc <args>
- admins/mods can say something official in a channel, it will also come with a notice to grab attention
- server admin password (set in config) allows global admin privileges
- channel registration to three passwords (founder/admin/mod)
  - only the founder and optionally some admins can regenerate these passwords
  - each channel password can be supplied during connection as server password (e.g. #lobby/swordfish:#lounge/8ball) or via a command
- private channels (+k implementation)
- implement read locks...? are they necessary?
- whois anonymous<#> easter egg, could be pre-programmed witty phrases/quotes
- op users (locally) when they are logged in to a founder/admin/mod password for client compatibility
