using System.Text.RegularExpressions;
using UndertaleModLib.Compiler;

EnsureDataLoaded();

// The phone D-pad uses I/J/K/L. Read them inside the battle menu only.
// Heart movement and other movement continue to use the original input scripts.
const string battleStep = "gml_Object_obj_battlecontroller_Step_0";
var source = GetDecompiledText(battleStep);
const string menu = "if (global.myfight == 0)\n{";
var menuStart = source.IndexOf(menu, StringComparison.Ordinal);
if (menuStart < 0)
    throw new InvalidOperationException("The original battle menu was not found.");

const string directions = @"\b(up|down|left|right)_p\(\)";
var keys = new Dictionary<string, string>
{
    ["up"] = "I",
    ["down"] = "K",
    ["left"] = "J",
    ["right"] = "L",
};

// myfight can change during this event as a player confirms a selection.
var changed = Regex.Replace(source.Substring(menuStart), directions, match =>
    $"({match.Value} || (global.myfight == 0 && keyboard_check_pressed(ord(\"{keys[match.Groups[1].Value]}\"))))");
if (changed == source.Substring(menuStart))
    throw new InvalidOperationException("The original battle menu direction checks were not found.");

var changes = new CodeImportGroup(Data);
changes.QueueReplace(battleStep, source.Substring(0, menuStart) + changed);
changes.Import();
