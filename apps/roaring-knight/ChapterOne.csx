using UndertaleModLib.Compiler;

var changes = new CodeImportGroup(Data);
const string start = @"
var yui_fight = 0;
for (var i = 1; i <= parameter_count(); i++)
{
    if (parameter_string(i) == ""yui-jevil"") yui_fight = 25;
    if (parameter_string(i) == ""yui-king"") yui_fight = 40;
}
if (yui_fight != 0)
{
    scr_gamestart();
    global.yui_boss = yui_fight;
    global.yui_state = 0;
    global.darkzone = 1;
    scr_getchar(2);
    scr_getchar(3);
    room_goto(yui_fight == 25 ? room_cc_joker : room_cc_kingbattle);
    exit;
}
";
changes.QueuePrepend("gml_Object_DEVICE_MENU_Step_0", start);
changes.QueuePrepend("gml_Object_DEVICE_CONTACT_Step_0", start);
changes.QueuePrepend("gml_Object_PROCESS_LOGO_Draw_0", start);
const string fight = @"
if (variable_global_exists(""yui_boss""))
{
    if (global.yui_state == 0)
    {
        visible = false;
        with (obj_joker_body) visible = false;
        snd_free_all();
        global.interact = 1;
        global.flag[9] = 1;
        global.batmusic[0] = snd_init(global.yui_boss == 25 ? ""joker.ogg"" : ""kingboss.ogg"");
        global.encounterno = global.yui_boss;
        global.specialbattle = 0;
        instance_create(0, 0, obj_encounterbasic);
        global.yui_state = 1;
    }
    else if (global.yui_state == 1 && instance_exists(obj_battlecontroller))
    {
        global.yui_state = 2;
    }
    else if (global.yui_state == 2 && (!instance_exists(obj_battlecontroller) || obj_battlecontroller.intro == 2))
    {
        global.yui_knight_result = 1;
        snd_free_all();
        audio_stop_all();
        room_goto(room_gameover);
    }
    exit;
}
";
changes.QueuePrepend("gml_Object_obj_jokerbattleevent_Step_0", fight);
changes.QueuePrepend("gml_Object_obj_kingcutscene_Step_0", fight);

const string gameOver = "gml_GlobalScript_scr_gameover";
var gameOverSource = GetDecompiledText(gameOver);
const string gameOverBody = "function scr_gameover()\n{";
var bodyStart = gameOverSource.IndexOf(gameOverBody, StringComparison.Ordinal);
if (bodyStart < 0) throw new InvalidOperationException("No original game-over function");
changes.QueueReplace(gameOver, gameOverSource.Insert(bodyStart + gameOverBody.Length, @"
    if (variable_global_exists(""yui_boss""))
    {
        global.yui_knight_result = 2;
        snd_free_all();
        audio_stop_all();
        room_goto(room_gameover);
        return;
    }
"));
changes.QueuePrepend("gml_Object_obj_gameover_init_Create_0", @"
if (variable_global_exists(""yui_knight_result"")) { visible = true; depth = -100000; exit; }
");
changes.QueuePrepend("gml_Object_obj_gameover_init_Step_0", @"
if (variable_global_exists(""yui_knight_result"")) exit;
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
