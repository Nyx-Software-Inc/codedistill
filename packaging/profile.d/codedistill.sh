# Installed by the codedistill / codedistill-server packages to
# /etc/profile.d/codedistill.sh — puts the CodeDistill CLI on PATH for
# login shells. The systemd units use the absolute path and don't need this.
case ":$PATH:" in
  *":/opt/CodeDistill/bin:"*) ;;
  *) PATH="$PATH:/opt/CodeDistill/bin" ;;
esac
export PATH
