$script:__go_cli_7c9bbe5ec9b3_complete = {
    param($CommandName, $ParameterName, $WordToComplete, $CommandAst, $FakeBoundParameters)
    $arguments = @('__complete')
    foreach ($element in $CommandAst.CommandElements | Select-Object -Skip 1) {
        if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst]) {
            $arguments += $element.Value
        } else {
            $arguments += $element.Extent.Text
        }
    }
    $output = & 'tool' @arguments 2>$null
    foreach ($line in $output) {
        if ($line -match '^:[0-9]+$') {
            continue
        }
        $parts = $line -split ([char]9), 2
        $candidate = $parts[0]
        $description = ''
        if ($parts.Length -gt 1) {
            $description = $parts[1]
        }
        [System.Management.Automation.CompletionResult]::new(
            $candidate, $candidate, 'ParameterValue', $description
        )
    }
}
Register-ArgumentCompleter -CommandName 'tool' -ScriptBlock $script:__go_cli_7c9bbe5ec9b3_complete
