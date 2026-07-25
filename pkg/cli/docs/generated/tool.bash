# bash completion for 'tool'
__go_cli_7c9bbe5ec9b3_complete() {
    local line
    COMPREPLY=()
    while IFS= read -r line; do
        if [[ "$line" =~ ^:[0-9]+$ ]]; then
            continue
        fi
        COMPREPLY+=("${line%%$'\t'*}")
    done < <(command "${COMP_WORDS[0]}" __complete \
        "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
}
complete -o default -F __go_cli_7c9bbe5ec9b3_complete -- 'tool'
