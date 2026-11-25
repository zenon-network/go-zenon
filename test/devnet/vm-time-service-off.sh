#!/usr/bin/env bash
# Turn off the Colima VM's own time service, so that only the Lima guest agent
# sets the VM's clock and the VM follows the host's. See docs/TESTING.md, P4.
#
# This CHANGES THE VM. Run it only on a VM you use for testing. It is undone
# when the VM is recreated, and may be undone by a VM restart; run it again
# then. It does not change the host.
#
# systemd-timesyncd steers the clock by changing the kernel's frequency
# correction, and that correction stays after the service stops; this resets
# it to 0 with adjtimex(2), through python3 in the VM (the VM has no adjtimex
# tool).
#
# It exits 0 only if, at the end, the VM reports NTP=no and the kernel's
# frequency correction reads back as exactly 0. Any failed step in the VM
# stops it with a non-zero exit.
set -euo pipefail

colima ssh -- sh -c 'set -eu
sudo timedatectl set-ntp false
sleep 1
echo "timesyncd: $(systemctl is-active systemd-timesyncd 2>&1 || true)"
sudo python3 - <<PY
import ctypes, sys
class Timex(ctypes.Structure):
    _fields_ = [("modes", ctypes.c_uint), ("offset", ctypes.c_long), ("freq", ctypes.c_long),
                ("maxerror", ctypes.c_long), ("esterror", ctypes.c_long), ("status", ctypes.c_int),
                ("constant", ctypes.c_long), ("precision", ctypes.c_long), ("tolerance", ctypes.c_long),
                ("time_sec", ctypes.c_long), ("time_usec", ctypes.c_long), ("tick", ctypes.c_long),
                ("pad", ctypes.c_long * 24)]
libc = ctypes.CDLL("libc.so.6", use_errno=True)
def read(label):
    t = Timex()
    if libc.adjtimex(ctypes.byref(t)) < 0:
        sys.exit("adjtimex read failed, errno %d" % ctypes.get_errno())
    print("%s: freq=%.3f ppm offset=%d status=0x%x" % (label, t.freq / 65536.0, t.offset, t.status))
    return t
read("before")
t = Timex()
t.modes = 0x0001 | 0x0002 | 0x0010   # ADJ_OFFSET | ADJ_FREQUENCY | ADJ_STATUS
t.freq = 0
t.offset = 0
t.status = 0x0040                    # STA_UNSYNC
if libc.adjtimex(ctypes.byref(t)) < 0:
    sys.exit("adjtimex set failed, errno %d" % ctypes.get_errno())
if read("after").freq != 0:
    sys.exit("the frequency correction did not read back as 0")
PY
ntp=$(timedatectl show -p NTP --value)
echo "NTP=$ntp"
[ "$ntp" = no ] || { echo "the VM still has NTP=$ntp" >&2; exit 1; }'
echo "vm-time-service-off: NTP=no and frequency correction 0 in the VM"
