using UndertaleModLib.Compiler;

EnsureDataLoaded();

var changes = new CodeImportGroup(Data);
const string autoLoad = @"
if (global.chapter == 3 && ossafe_file_exists(""filech3_0""))
{
    global.filechoice = 0;
    scr_load();
    global.yui_knight_practice = true;
    global.yui_boss = 115;
    for (var i = 1; i <= parameter_count(); i++)
    {
        if (parameter_string(i) == ""yui-tenna"")
            global.yui_boss = 121;
        if (parameter_string(i) == ""yui-eram"")
            global.yui_boss = 118;
    }
    if (global.yui_boss == 118)
    {
        global.interact = 0;
        global.flag[1002] = 1;
        global.swordscreencolor = #002DFF;
        room_goto(room_shadowmantle);
    }
    else if (global.yui_boss == 121)
    {
        global.plot = 300;
        room_goto(room_dw_snow_zone_battle);
    }
    else
    {
        global.tempflag[90] = 4;
    }
    exit;
}
";

changes.QueuePrepend("gml_Object_obj_intro_ch3_Step_0", autoLoad);
changes.QueuePrepend("gml_Object_DEVICE_MENU_Step_0", autoLoad);

// The game's retry setup positions the party and creates the encounter actors.
// Use its shortened branch on the first launch without changing the loss count.
const string encounter = "gml_Object_obj_ch3_PTB02_Create_0";
var source = GetDecompiledText(encounter);
const string retryBranch = "if (shorten)";
const string retryWait = "c_wait(60);";
var branchStart = source.IndexOf(retryBranch, StringComparison.Ordinal);
var waitStart = branchStart < 0 ? -1 : source.IndexOf(retryWait, branchStart, StringComparison.Ordinal);
if (waitStart < 0)
    throw new InvalidOperationException("The original Roaring Knight retry setup was not found.");

source = source
    .Remove(waitStart, retryWait.Length)
    .Insert(waitStart, "c_wait(variable_global_exists(\"yui_knight_practice\") ? 1 : 60);")
    .Remove(branchStart, retryBranch.Length)
    .Insert(branchStart, "if (shorten || variable_global_exists(\"yui_knight_practice\"))");
changes.QueueReplace(encounter, source);

const string tenna = "gml_Object_obj_ch3_BTB06_Step_0";
var tennaSource = GetDecompiledText(tenna);
const string tennaSetup = "    te_actor.preset = 0;\n    c_sel(kr);";
var setupIndex = tennaSource.IndexOf(tennaSetup, StringComparison.Ordinal);
var nextScene = tennaSource.IndexOf("\n}\nif (con == 50)", StringComparison.Ordinal);
if (setupIndex < 0 || nextScene < setupIndex)
    throw new InvalidOperationException("The original Tenna setup was not found.");

tennaSource = tennaSource.Insert(nextScene, "\n    }");
tennaSource = tennaSource.Insert(setupIndex + "    te_actor.preset = 0;".Length, @"
    if (variable_global_exists(""yui_boss"") && global.yui_boss == 121)
    {
        kr_actor.x = camerax() + 200;
        kr_actor.y = 200;
        su_actor.x = camerax() + 124;
        su_actor.y = 188;
        ra_actor.x = camerax() + 60;
        ra_actor.y = 188;
        customcon = 1;
    }
    else
    {");
changes.QueueReplace(tenna, @"
if (variable_global_exists(""yui_boss"") && global.yui_boss == 121)
{
    if (con == 0)
    {
        con = 2;
        global.interact = 1;
        global.facing = 0;
    }
    if (con == 19)
    {
        global.yui_knight_result = 1;
        snd_free_all();
        audio_stop_all();
        room_goto(room_gameover);
        exit;
    }
}
" + tennaSource);

// ERAM is a board fight, with a separate victory actor and health system.
changes.QueuePrepend("gml_Object_obj_shadow_mantle_enemy_outro_Create_0", @"
if (variable_global_exists(""yui_boss"") && global.yui_boss == 118)
{
    global.yui_knight_result = 1;
    snd_free_all();
    audio_stop_all();
    room_goto(room_gameover);
    exit;
}
");
changes.QueuePrepend("gml_Object_obj_mainchara_board_Step_0", @"
if (variable_global_exists(""yui_boss"") && global.yui_boss == 118 && myhealth <= 0)
{
    global.yui_knight_result = 2;
    snd_free_all();
    audio_stop_all();
    room_goto(room_gameover);
    exit;
}
");

// The encounter reports both a victory and its scripted first loss here,
// before it writes story progress or starts the closing cutscene.
changes.QueuePrepend("gml_Object_obj_ch3_PTB02_Step_0", @"
if (con == 8 && variable_global_exists(""yui_knight_practice""))
{
    global.yui_knight_result = teamdefeated ? 2 : 1;
    snd_free_all();
    audio_stop_all();
    room_goto(room_gameover);
    exit;
}
");

// With prior attempts, a loss uses the general game-over path instead.
const string gameOver = "gml_GlobalScript_scr_gameover";
var gameOverSource = GetDecompiledText(gameOver);
const string gameOverBody = "function scr_gameover()\n{";
var bodyStart = gameOverSource.IndexOf(gameOverBody, StringComparison.Ordinal);
if (bodyStart < 0)
    throw new InvalidOperationException("The original game-over function was not found.");
changes.QueueReplace(gameOver, gameOverSource.Insert(bodyStart + gameOverBody.Length, @"
    if (variable_global_exists(""yui_boss"") && (global.tempflag[93] == 1 || global.yui_boss == 121))
    {
        global.yui_knight_result = 2;
        snd_free_all();
        audio_stop_all();
        room_goto(room_gameover);
        return;
    }
"));

// The result uses the existing game-over room without its story or input handling.
changes.QueuePrepend("gml_Object_obj_gameover_init_Create_0", @"
if (variable_global_exists(""yui_knight_result""))
{
    visible = true;
    depth = -100000;
    exit;
}
");
changes.QueuePrepend("gml_Object_obj_gameover_init_Step_0", @"
if (variable_global_exists(""yui_knight_result""))
    exit;
");
changes.QueueReplace("gml_Object_obj_gameover_init_Draw_0", @"
if (variable_global_exists(""yui_knight_result""))
{
    draw_clear(c_black);
    draw_set_alpha(1);
    draw_set_color(c_white);
    draw_set_font(-1);
    draw_set_halign(fa_center);
    draw_set_valign(fa_middle);
    draw_text_transformed(room_width / 2, room_height / 2 - 20,
        global.yui_knight_result == 1 ? ""Victory"" : ""Defeated"", 2, 2, 0);
    draw_text(room_width / 2, room_height / 2 + 20, ""Choose a fight in Yui to play again."");
    draw_set_halign(fa_left);
    draw_set_valign(fa_top);
}
");
changes.Import();
