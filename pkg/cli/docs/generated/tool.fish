function __go_cli_7c9bbe5ec9b3_complete
    set -l tokens (commandline -opc)
    set -l current (commandline -ct)
    set -a tokens "$current"
    command $tokens[1] __complete $tokens[2..-1] 2>/dev/null |
        string match -rv '^:[0-9]+$'
end
complete -c 'tool' -f -a '(__go_cli_7c9bbe5ec9b3_complete)'
