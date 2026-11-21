using System.Text.RegularExpressions;
using UndertaleModLib.Compiler;

EnsureDataLoaded();

// These keys are reserved for camera input. The battle menus still read
// their original controls, as do the boxing and board-game characters.
const string heart = "gml_Object_obj_heart_Step_0";
var source = GetDecompiledText(heart);
var keys = new Dictionary<string, string>
{
    ["up"] = "T",
    ["down"] = "G",
    ["left"] = "F",
    ["right"] = "H",
};
foreach (var direction in keys)
{
    var input = $"\\b{direction.Key}_h\\(\\)";
    if (Regex.Matches(source, input).Count != 1)
        throw new InvalidOperationException($"Expected one {direction.Key} input in the heart Step event.");
    source = Regex.Replace(source, input, match =>
        $"({match.Value} || keyboard_check(ord(\"{direction.Value}\")))");
}

var changes = new CodeImportGroup(Data);
changes.QueueReplace(heart, source);
changes.Import();
