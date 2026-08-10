#!/usr/bin/env bats

setup_file() {
	PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/.." && pwd)"
	export PROJECT_ROOT

	ORIGINAL_HOME="${HOME:-}"
	export ORIGINAL_HOME

	HOME="$(mktemp -d "${BATS_TEST_DIRNAME}/tmp-optimize-db.XXXXXX")"
	export HOME
}

teardown_file() {
	if [[ "$HOME" == "${BATS_TEST_DIRNAME}/tmp-"* ]]; then
		rm -rf "$HOME"
	fi
	if [[ -n "${ORIGINAL_HOME:-}" ]]; then
		export HOME="$ORIGINAL_HOME"
	fi
}

create_logical_file() {
	local path="$1"
	local size="$2"

	if command -v mkfile > /dev/null 2>&1; then
		mkfile -n "$size" "$path"
	else
		truncate -s "$size" "$path"
	fi
}

@test "opt_notification_cleanup reports healthy when db is small" {
	local tmp_dir nc_db_dir
	tmp_dir=$(mktemp -d)
	# Legacy Darwin-user-dir layout (pre-Sequoia fallback).
	nc_db_dir="$tmp_dir/com.apple.notificationcenter/db2"
	mkdir -p "$nc_db_dir"
	create_logical_file "$nc_db_dir/db" 1k

	run env HOME="$HOME" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<EOF
set -euo pipefail
source "\$PROJECT_ROOT/lib/core/common.sh"
source "\$PROJECT_ROOT/lib/optimize/tasks.sh"
getconf() { echo "$tmp_dir"; }
execute_optimization notification_cleanup
EOF

	rm -rf "$tmp_dir"
	[ "$status" -eq 0 ]
	[[ "$output" == *"healthy"* ]]
}

@test "opt_notification_cleanup prefers the usernoted group-container database" {
	# macOS 15+ live path (issue #1368). When both exist, prefer group container.
	# Isolated HOME so leftover group-container dbs do not poison sibling tests.
	local case_home group_dir legacy_root legacy_dir
	case_home=$(mktemp -d "${BATS_TEST_DIRNAME}/tmp-nc-group.XXXXXX")
	group_dir="$case_home/Library/Group Containers/group.com.apple.usernoted/db2"
	legacy_root=$(mktemp -d "${BATS_TEST_DIRNAME}/tmp-nc-legacy.XXXXXX")
	legacy_dir="$legacy_root/com.apple.notificationcenter/db2"
	mkdir -p "$group_dir" "$legacy_dir"
	create_logical_file "$group_dir/db" 1k
	create_logical_file "$legacy_dir/db" 1k

	run env HOME="$case_home" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<EOF
set -euo pipefail
source "\$PROJECT_ROOT/lib/core/common.sh"
source "\$PROJECT_ROOT/lib/optimize/tasks.sh"
getconf() { echo "$legacy_root"; }
debug_log() { printf 'DEBUG:%s\n' "\$*"; }
execute_optimization notification_cleanup
EOF

	rm -rf "$case_home" "$legacy_root"
	[ "$status" -eq 0 ] || {
		echo "$output"
		return 1
	}
	[[ "$output" == *"healthy"* ]] || return 1
	[[ "$output" == *"DEBUG:Notification Center database: $group_dir/db"* ]] || return 1
}

@test "opt_notification_cleanup reports unavailable when no supported path exists" {
	run env HOME="$HOME/nc-missing" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
getconf() { echo "/nonexistent-darwin-user-dir"; }
execute_optimization notification_cleanup
[[ "$(optimize_outcome_count unavailable)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || {
		echo "$output"
		return 1
	}
	[[ "$output" == *"unavailable"* ]] || return 1
	[[ "$output" != *"not found"* ]] || return 1
	[[ "$output" != *"already optimized"* ]] || return 1
}

@test "opt_notification_cleanup warns when sqlite3 fails" {
	local tmp_dir nc_db_dir
	tmp_dir=$(mktemp -d)
	nc_db_dir="$tmp_dir/com.apple.notificationcenter/db2"
	mkdir -p "$nc_db_dir"
	create_logical_file "$nc_db_dir/db" 60m

	run env HOME="$HOME" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<EOF
set -euo pipefail
source "\$PROJECT_ROOT/lib/core/common.sh"
source "\$PROJECT_ROOT/lib/optimize/tasks.sh"
getconf() { echo "$tmp_dir"; }
sqlite3() { return 1; }
execute_optimization notification_cleanup
EOF

	rm -rf "$tmp_dir"
	[ "$status" -eq 0 ]
	[[ "$output" == *"busy or locked"* ]]
}

@test "opt_coreduet_cleanup reports healthy when db is small" {
	local tmp_dir
	tmp_dir=$(mktemp -d)
	mkdir -p "$tmp_dir/Library/Application Support/Knowledge"
	local knowledge_db="$tmp_dir/Library/Application Support/Knowledge/knowledgeC.db"
	create_logical_file "$knowledge_db" 1k

	run env HOME="$tmp_dir" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<EOF
set -euo pipefail
source "\$PROJECT_ROOT/lib/core/common.sh"
source "\$PROJECT_ROOT/lib/optimize/tasks.sh"
execute_optimization coreduet_cleanup
EOF

	rm -rf "$tmp_dir"
	[ "$status" -eq 0 ]
	[[ "$output" == *"healthy"* ]]
}

@test "opt_coreduet_cleanup warns when sqlite3 fails" {
	local tmp_dir fake_bin
	tmp_dir=$(mktemp -d)
	fake_bin="$tmp_dir/bin"
	mkdir -p "$tmp_dir/Library/Application Support/Knowledge" "$fake_bin"
	local knowledge_db="$tmp_dir/Library/Application Support/Knowledge/knowledgeC.db"
	create_logical_file "$knowledge_db" 1k

	cat > "$fake_bin/du" <<'EOF'
#!/bin/bash
echo "112640 total"
EOF
	chmod +x "$fake_bin/du"

	run env HOME="$tmp_dir" PROJECT_ROOT="$PROJECT_ROOT" PATH="$fake_bin:$PATH" /bin/bash --noprofile --norc <<EOF
set -euo pipefail
source "\$PROJECT_ROOT/lib/core/common.sh"
source "\$PROJECT_ROOT/lib/optimize/tasks.sh"
sqlite3() { return 1; }
execute_optimization coreduet_cleanup
EOF

	rm -rf "$tmp_dir"
	[ "$status" -eq 0 ]
	[[ "$output" == *"busy or locked"* ]]
}

@test "SQLite optimization is unavailable when pgrep is missing" {
	run env HOME="$HOME/sqlite-no-pgrep" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
sqlite3() { echo "UNEXPECTED_SQLITE"; return 0; }
PATH=/nonexistent

execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count unavailable)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"process probe unavailable"* ]] || return 1
	[[ "$output" != *"UNEXPECTED_SQLITE"* ]]
}

@test "SQLite optimization fails closed when pgrep errors" {
	run env HOME="$HOME/sqlite-pgrep-error" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
pgrep() { return 2; }
sqlite3() { echo "UNEXPECTED_SQLITE"; return 0; }

execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count failed)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"Failed to inspect active apps"* ]] || return 1
	[[ "$output" != *"UNEXPECTED_SQLITE"* ]]
}

@test "SQLite optimization skips while an owning app is running" {
	run env HOME="$HOME/sqlite-busy" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
pgrep() {
    [[ "$1" == "-x" && "$2" == "Mail" ]]
}
sqlite3() { echo "UNEXPECTED_SQLITE"; return 0; }

execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count skipped)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"Close these apps before database optimization: Mail"* ]] || return 1
	[[ "$output" != *"UNEXPECTED_SQLITE"* ]]
}

@test "SQLite optimization proceeds after reliable no-match process probes" {
	run env HOME="$HOME/sqlite-clear" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
pgrep() { return 1; }
sqlite3() { :; }
file() { echo "SQLite 3.x database"; }
get_file_size() { echo 1024; }
should_protect_path() { return 1; }
run_with_timeout() {
    case "$4" in
        "PRAGMA page_count; PRAGMA freelist_count; PRAGMA page_size;") printf '100\n10\n4096\n' ;;
        "PRAGMA integrity_check;") echo "ok" ;;
        "VACUUM;") echo "VACUUM_CALLED" ;;
        *)
            [[ "$2" == "df" ]] || return 64
            printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/mock 2000000 100000 1900000 5%% /\n'
            ;;
    esac
}

execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count applied)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"VACUUM_CALLED"* ]] || return 1
	[[ "$output" == *"Optimized 1 databases"* ]]
}

@test "SQLite optimization does not claim all optimized when size policy skips" {
	# issue #1367: oversized skip must not share the "already optimized" headline.
	run env HOME="$HOME/sqlite-oversized" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
db2="$HOME/Library/Safari/History.db"
mkdir -p "$(dirname "$db2")"
touch "$db2"
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
# 200 MiB > MOLE_SQLITE_MAX_SIZE (100 MiB).
get_file_size() { echo 209715200; }
should_protect_path() { return 1; }
bytes_to_human() { echo "200.0MB"; }
run_with_timeout() { printf '100000\n10000\n4096\n'; }

execute_optimization sqlite_vacuum
EOF

	[ "$status" -eq 0 ] || {
		echo "$output"
		return 1
	}
	[[ "$output" == *"No databases compacted"* ]] || return 1
	[[ "$output" == *"100MiB safety limit"* ]] || return 1
	[[ "$output" == *"Messages/chat.db"* ]] || return 1
	[[ "$output" != *"All databases already optimized"* ]] || return 1
	[[ "$output" != *"UNEXPECTED_SQLITE"* ]] || return 1
}

@test "SQLite size override is bounded and unit-bearing" {
	run env PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
optimize_set_database_max_size 300MiB
[[ "$MOLE_SQLITE_MAX_SIZE" -eq 314572800 ]] || exit 1
[[ "$MOLE_SQLITE_MAX_SIZE_DISPLAY" == "300MiB" ]] || exit 1
for value in 0MiB 300 2GiB -1MiB 999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999KiB; do
    if optimize_set_database_max_size "$value"; then
        exit 1
    fi
done
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
}

@test "SQLite size override allows a reclaimable large curated database" {
	run env HOME="$HOME/sqlite-opt-in" PROJECT_ROOT="$PROJECT_ROOT" MOLE_DRY_RUN=1 /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
optimize_set_database_max_size 300MiB
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
get_file_size() { echo 209715200; }
should_protect_path() { return 1; }
run_with_timeout() { printf '100000\n10000\n4096\n'; }
execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count applied)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"Optimized 1 databases"* ]] || return 1
}

@test "SQLite compact large database is classified before size policy" {
	run env HOME="$HOME/sqlite-compact-large" PROJECT_ROOT="$PROJECT_ROOT" MOLE_DRY_RUN=1 /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
get_file_size() { echo 209715200; }
should_protect_path() { return 1; }
run_with_timeout() { printf '100000\n100\n4096\n'; }
execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count unchanged)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"Already optimal for 1 databases"* ]] || return 1
}

@test "SQLite file size probe failures are reported without aborting" {
	run env HOME="$HOME/sqlite-size-failure" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
get_file_size() { return 124; }
should_protect_path() { return 1; }
execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count failed)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"Failed on 1 databases"* ]] || return 1
}

@test "SQLite size probe interruption propagates and stops the task" {
	run env HOME="$HOME/sqlite-size-interrupted" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
db2="$HOME/Library/Safari/History.db"
mkdir -p "$(dirname "$db2")"
touch "$db2"
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
should_protect_path() { return 1; }
calls_log="$HOME/size-probe-calls"
get_file_size() {
    printf '%s\n' "$1" >> "$calls_log"
    return 130
}
rc=0
optimize_task_start
opt_sqlite_vacuum || rc=$?
[[ "$rc" -eq 130 ]] || exit 1
[[ "$(wc -l < "$calls_log")" -eq 1 ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
}

@test "SQLite extreme PRAGMA values fail closed without arithmetic overflow" {
	run env HOME="$HOME/sqlite-extreme" PROJECT_ROOT="$PROJECT_ROOT" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
get_file_size() { echo 209715200; }
should_protect_path() { return 1; }
run_with_timeout() { printf '1\n999999999999999999999\n4096\n'; }
execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count failed)" == "1" ]] || exit 1
EOF

	[ "$status" -eq 0 ] || { echo "$output"; return 1; }
	[[ "$output" == *"Failed on 1 databases"* ]] || return 1
}

@test "SQLite temporary-space probe failures are reported separately" {
	local mode
	for mode in timeout malformed low; do
		run env HOME="$HOME/sqlite-space-$mode" PROJECT_ROOT="$PROJECT_ROOT" SPACE_MODE="$mode" /bin/bash --noprofile --norc <<'EOF'
set -euo pipefail
source "$PROJECT_ROOT/lib/core/common.sh"
source "$PROJECT_ROOT/lib/optimize/tasks.sh"
db="$HOME/Library/Messages/chat.db"
mkdir -p "$(dirname "$db")"
touch "$db"
pgrep() { return 1; }
file() { echo "SQLite 3.x database"; }
get_file_size() { echo 1024; }
should_protect_path() { return 1; }
run_with_timeout() {
    case "$4" in
        "PRAGMA page_count; PRAGMA freelist_count; PRAGMA page_size;") printf '100\n10\n4096\n' ;;
        "PRAGMA integrity_check;") echo ok ;;
        *)
            if [[ "$SPACE_MODE" == timeout ]]; then return 124; fi
            if [[ "$SPACE_MODE" == malformed ]]; then printf 'bad\n'; return 0; fi
            printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/mock 2000 1999 1 99%% /\n'
            ;;
    esac
}
execute_optimization sqlite_vacuum
[[ "$(optimize_outcome_count failed)" == "1" ]] || exit 1
EOF

		[ "$status" -eq 0 ] || { echo "$output"; return 1; }
		if [[ "$mode" == low ]]; then
			[[ "$output" == *"Insufficient temporary space"* ]] || return 1
		else
			[[ "$output" == *"Unable to verify temporary space"* ]] || return 1
		fi
	done
}
