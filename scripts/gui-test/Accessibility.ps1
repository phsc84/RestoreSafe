# Accessibility.ps1 - lists what screen readers see of a window: the role,
# name, and keyboard shortcut of every visible control, through MSAA
# (IAccessible), which is how screen readers read classic Win32 controls.
#
# Dot-source it after GuiDriver.ps1:  . .\GuiDriver.ps1; . .\Accessibility.ps1
# Then:  Show-Accessibility $mainWindowHandle

Add-Type -AssemblyName Accessibility
Add-Type -ReferencedAssemblies Accessibility @"
using System; using System.Runtime.InteropServices; using Accessibility;
public static class Msaa {
  [DllImport("oleacc.dll")] static extern int AccessibleObjectFromWindow(IntPtr h, uint id, ref Guid iid, [MarshalAs(UnmanagedType.Interface)] out object o);
  [DllImport("oleacc.dll", CharSet=CharSet.Unicode)] static extern uint GetRoleText(uint role, System.Text.StringBuilder s, uint n);
  public static string Describe(IntPtr h) {
    Guid iid = new Guid("618736E0-3C3D-11CF-810C-00AA00389B71"); object o;
    if (AccessibleObjectFromWindow(h, 0xFFFFFFFC, ref iid, out o) != 0) return "(no IAccessible)";
    var a = (IAccessible)o;
    string name = "", key = ""; uint role = 0;
    try { name = a.get_accName(0); } catch {}
    try { key = a.get_accKeyboardShortcut(0); } catch {}
    try { role = Convert.ToUInt32(a.get_accRole(0)); } catch {}
    var sb = new System.Text.StringBuilder(64); GetRoleText(role, sb, 64);
    return string.Format("{0,-22} '{1}'{2}", sb, name, string.IsNullOrEmpty(key) ? "" : "  [" + key + "]");
  }
}
"@

# Show-Accessibility prints role, name, and shortcut of every visible child
# control of $parent, in creation (tab) order.
function Show-Accessibility([IntPtr]$parent) {
  foreach ($h in [U]::Children($parent)) { if ([U]::IsWindowVisible($h)) { [Msaa]::Describe($h) } }
}
