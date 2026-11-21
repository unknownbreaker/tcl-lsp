//! parsedump [ROOT] OUT: writes OUT/<rel>.json, the dump of every *.tcl file
//! under ROOT (default $TCL_CORPUS, else the Homebrew Tcl library).

use std::path::PathBuf;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let (root, out) = match args.as_slice() {
        [out] => (tclparse::corpus_root(), PathBuf::from(out)),
        [root, out] => (PathBuf::from(root), PathBuf::from(out)),
        _ => {
            eprintln!("usage: parsedump [ROOT] OUT");
            std::process::exit(2);
        }
    };
    tclparse::with_big_stack(move || {
        let files = tclparse::load_corpus(&root).unwrap_or_else(|e| {
            eprintln!("parsedump: {}: {e}", root.display());
            std::process::exit(1);
        });
        for (rel, src) in &files {
            let p = out.join(format!("{rel}.json"));
            std::fs::create_dir_all(p.parent().unwrap()).unwrap();
            std::fs::write(&p, tclparse::dump(src)).unwrap();
        }
        eprintln!("parsedump: {} files", files.len());
    });
}
