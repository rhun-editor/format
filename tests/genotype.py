#!/usr/bin/env python3
"""Real gt integration: unsaved text, undo, selection, save, invalid source and on-type edits.

Usage: RHUN_GENOTYPE_CLI=/path/to/gt python3 tests/genotype.py /path/to/rhun
The CLI is already installed; this test never downloads it or touches user configuration.
"""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
EXE = Path(sys.argv.pop(1) if len(sys.argv) > 1 else shutil.which('rhun') or 'rhun').resolve()
GT = os.environ.get('RHUN_GENOTYPE_CLI') or shutil.which('gt')
if not GT:
    print('skip genotype integration (requires Genotype CLI 0.18.0 or later)')
    raise SystemExit(0)
GT = Path(GT).resolve()
MOD = 'cmd' if sys.platform == 'darwin' else 'ctrl'
BASE = ('[ui]\nsidebar = false\nagents_panel = false\nfollow_system = false\n'
        '[editor]\ncursor_blink = false\nauto_pairs = false\nformat_on_type = {on_type}\n'
        '[files]\nrestore_session = false\nrestore_project = false\nformat_on_save = {on_save}\n'
        '[updates]\ncheck = false\n[git]\nenabled = false\n')


class Genotype(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='rhun-format-genotype-')
        cls.root = Path(cls.temporary.name).resolve()
        cls.exts = cls.root / 'extensions'
        extension = cls.exts / 'rhun-editor/format'
        (extension / 'bin').mkdir(parents=True)
        target = 'windows-x64.exe' if os.name == 'nt' else 'macos-arm64' if sys.platform == 'darwin' else 'linux-x86_64'
        binary = extension / 'bin' / ('format-' + target)
        prebuilt = os.environ.get('RHUN_FORMAT_TEST_BIN')
        if prebuilt:
            shutil.copyfile(prebuilt, binary)
            binary.chmod(0o755)
        else:
            subprocess.run(['go', 'build', '-o', str(binary), '.'],
                           cwd=ROOT, env=dict(os.environ, CGO_ENABLED='0'), check=True)
        shutil.copyfile(ROOT / 'extension.ini', extension / 'extension.ini')
        shutil.copyfile(ROOT / 'icon.png', extension / 'icon.png')
        cls.tools = cls.root / 'tools'
        cls.tools.mkdir()
        cli = cls.tools / ('gt.exe' if os.name == 'nt' else 'gt')
        shutil.copyfile(GT, cli)
        cli.chmod(0o755)

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def setUp(self):
        self.temporary_case = tempfile.TemporaryDirectory(dir=self.root)
        self.work = Path(self.temporary_case.name)
        self.project = self.work / 'project café with spaces'
        self.project.mkdir()
        self.file = self.project / 'model.type'
        self.file.write_text('User:{name:string}', encoding='utf-8')
        self.peer = self.project / 'other.type'
        self.peer.write_text('Other:{untouched:boolean}', encoding='utf-8')
        self.config = self.work / 'config/rhun/config'
        self.config.parent.mkdir(parents=True)
        # Older rhun releases can exercise the formatter independently of the new built-in grammar.
        syntax = self.work / 'config/rhun/syntax'
        syntax.mkdir()
        (syntax / 'genotype.syn').write_text('name = Genotype\nfiles = *.type\n')
        self.env = dict(os.environ, HOME=str(self.work), XDG_CONFIG_HOME=str(self.work / 'config'),
                        XDG_STATE_HOME=str(self.work / 'state'),
                        PATH=str(self.tools) + os.pathsep + os.environ['PATH'])

    def tearDown(self):
        self.temporary_case.cleanup()

    def editor(self, actions, on_save=False, on_type=False):
        self.config.write_text(BASE.format(on_save=str(on_save).lower(), on_type=str(on_type).lower()))
        script = self.work / 'actions.rsc'
        script.write_text('\n'.join(['wait 100', 'open ' + str(self.file), *actions, 'quit']) + '\n',
                          encoding='utf-8')
        result = subprocess.run([str(EXE), str(self.project), '--headless', '1000x600', '--scale', '1',
                                 '--extensions', str(self.exts), '--script', str(script)],
                                env=self.env, capture_output=True, text=True, encoding='utf-8', timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.peer.read_text(), 'Other:{untouched:boolean}')
        self.assertFalse(list((self.work / 'state').rglob('genotype-*')), 'temporary buffers were not removed')
        documents, current = [], []
        for line in result.stdout.splitlines():
            if line == '<eod>':
                documents.append('\n'.join(current))
                current = []
            else:
                current.append(line)
        return documents, result.stdout

    def test_unsaved_buffer_and_single_undo(self):
        source = 'User:{name:string,note:"café"}'
        documents, _ = self.editor(['cmd select_all', 'type ' + source,
                                    'key shift+alt+f', 'wait 1500', 'print-doc',
                                    f'key {MOD}+z', 'print-doc'])
        self.assertEqual(documents, ['User: { name: string, note: "café", }\n', source])
        self.assertEqual(self.file.read_text(), 'User:{name:string}')

    def test_format_on_save(self):
        documents, _ = self.editor(['cmd select_all', 'type User:{name:string,note:"saved"}',
                                    'cmd save', 'wait 1500', 'print-doc'], on_save=True)
        self.assertEqual(documents, ['User: { name: string, note: "saved", }\n'])
        self.assertEqual(self.file.read_text(), 'User: { name: string, note: "saved", }\n')

    def test_selection_only(self):
        self.file.write_text('User:{name:string}\n\nOther:{ready:boolean}')
        documents, _ = self.editor(['key ctrl+Home', 'key Home', 'key shift+End',
                                    'cmd format_selection', 'wait 1500', 'print-doc'])
        self.assertEqual(documents, ['User: { name: string, }\n\nOther:{ready:boolean}'])

    def test_invalid_source_is_unchanged(self):
        documents, output = self.editor(['cmd select_all', 'type Broken: {',
                                         'key shift+alt+f', 'wait 1500', 'print-doc', 'print-toast'])
        self.assertEqual(documents, ['Broken: {'])
        self.assertIn('syntax error', output.lower())
        self.assertNotIn('\x1b', output)
        self.assertEqual(self.file.read_text(), 'User:{name:string}')

    def test_optional_format_on_type(self):
        # On-type formatting uses an already running provider; it does not cold-start one.
        documents, _ = self.editor(['cmd format.format_document', 'wait 500',
                                    'cmd select_all', 'type User:{name:string', 'type }',
                                    'wait 1500', 'print-doc'], on_type=True)
        self.assertEqual(documents, ['User: { name: string, }\n'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
