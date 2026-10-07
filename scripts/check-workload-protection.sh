#!/bin/sh
# Run on the deployed Ubuntu server after FlowPanel has started.
set -eu

slice=flowpanel-workloads.slice
group=/sys/fs/cgroup/flowpanel.slice/$slice
php_slice=flowpanel-workloads-php.slice
apps_slice=flowpanel-workloads-apps.slice
for file in cpu.max memory.max pids.max; do
  value=$(cat "$group/$file")
  case "$value" in
    ''|max*) printf 'FAIL: %s has no effective limit\n' "$file" >&2; exit 1 ;;
  esac
  printf '%s: %s\n' "$file" "$value"
done
[ "$(cat "$group/memory.swap.max")" = 0 ]

php_memory=$(cat "$group/$php_slice/memory.low")
apps_memory=$(cat "$group/$apps_slice/memory.max")
shared_memory=$(cat "$group/memory.max")
[ "$php_memory" -gt 0 ]
[ "$(cat "$group/memory.low")" -ge "$php_memory" ]
[ "$(cat /sys/fs/cgroup/flowpanel.slice/memory.low)" -ge "$php_memory" ]
[ "$((apps_memory + php_memory))" -le "$shared_memory" ]
[ "$(cat "$group/$apps_slice/pids.max")" -le "$(( $(cat "$group/pids.max") - 512 ))" ]
php_cpu=$(cat "$group/$php_slice/cpu.weight")
apps_cpu=$(cat "$group/$apps_slice/cpu.weight")
[ "$php_cpu" -gt 0 ]
[ "$apps_cpu" -gt 0 ]
[ "$((php_cpu + apps_cpu))" -eq 100 ]
printf 'PHP CPU weight: %s; other workloads: %s; PHP RAM baseline: %s bytes\n' "$php_cpu" "$apps_cpu" "$php_memory"

php_units=$(systemctl list-units --type=service --state=running --no-legend 'php*-fpm.service')
printf '%s\n' "$php_units" | while read -r unit rest; do
  [ -n "$unit" ] || continue
  php_group=$(systemctl show "$unit" --property=ControlGroup --value)
  case "$php_group" in
    *"/$php_slice/"*) ;;
    *) printf 'FAIL: %s is outside the PHP budget\n' "$unit" >&2; exit 1 ;;
  esac
  [ "$(cat "/sys/fs/cgroup$php_group/memory.low")" = max ]
done

admin_group=$(systemctl show flowpanel.service --property=ControlGroup --value)
case "$admin_group" in
  '') printf 'FAIL: FlowPanel is not running\n' >&2; exit 1 ;;
  *"/$slice/"*) printf 'FAIL: FlowPanel is inside the workload budget\n' >&2; exit 1 ;;
esac

systemd-run --scope --quiet --collect --expand-environment=no --slice="$apps_slice" \
  --property=TasksMax=512 -- /bin/sh -ec '
    case "$(cat /proc/self/cgroup)" in
      */flowpanel-workloads-apps.slice/*) ;;
      *) echo "FAIL: command is outside the workload budget" >&2; exit 1 ;;
    esac
  '
printf 'PASS: kernel ceilings and PHP baseline active; workload command isolated; admin outside budget\n'
