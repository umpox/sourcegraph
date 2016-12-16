import { KeyCode, KeyMod } from "vs/base/common/keyCodes";
import { IDisposable } from "vs/base/common/lifecycle";
import URI from "vs/base/common/uri";
import { IEditorConstructionOptions, IStandaloneCodeEditor } from "vs/editor/browser/standalone/standaloneCodeEditor";
import { createModel } from "vs/editor/browser/standalone/standaloneEditor";
import { Position } from "vs/editor/common/core/position";
import { ICursorSelectionChangedEvent, IModelChangedEvent, IRange } from "vs/editor/common/editorCommon";
import { HoverOperation } from "vs/editor/contrib/hover/browser/hoverOperation";
import { MenuId, MenuRegistry } from "vs/platform/actions/common/actions";
import { IEditor } from "vs/platform/editor/common/editor";

import { code_font_face } from "sourcegraph/components/styles/_vars.css";
import { URIUtils } from "sourcegraph/core/uri";
import { EditorService, IEditorOpenedEvent } from "sourcegraph/editor/EditorService";
import * as lsp from "sourcegraph/editor/lsp";
import { modes } from "sourcegraph/editor/modes";
import { createEditor } from "sourcegraph/editor/setup";
import * as AnalyticsConstants from "sourcegraph/util/constants/AnalyticsConstants";
import { Features } from "sourcegraph/util/features";
import { isSupportedMode } from "sourcegraph/util/supportedExtensions";

import "sourcegraph/editor/contrib";
import "sourcegraph/editor/FindExternalReferencesAction";
import "sourcegraph/editor/GotoDefinitionWithClickEditorContribution";
import "sourcegraph/editor/vscode";
import "sourcegraph/workbench/overrides/labels";
import "vs/editor/common/editorCommon";
import "vs/editor/contrib/codelens/browser/codelens";

// Editor wraps the Monaco code editor.
export class Editor implements IDisposable {
	private _editor: IStandaloneCodeEditor;
	private _editorService: EditorService;
	private _toDispose: IDisposable[] = [];

	constructor(
		elem: HTMLElement
	) {
		HoverOperation.HOVER_TIME = 200;

		let initialModel = createModel("", "text/plain");
		[this._editor, this._editorService] = createEditor(elem, {
			// If we don't specify an initial model, Monaco will
			// create this one anyway (but it'll try to call
			// window.monaco.editor.createModel, and we don't want to
			// add any implicit dependency on window).
			model: initialModel,

			readOnly: true,
			automaticLayout: true,
			scrollBeyondLastLine: false,
			wrappingColumn: 0,
			fontFamily: code_font_face,
			fontSize: 15,
			lineHeight: 21,
			theme: "vs-dark",
			renderLineHighlight: "line",
			codeLens: Features.codeLens.isEnabled(),
			glyphMargin: false,
		});

		// WORKAROUND: Remove the initial model from the configuration to avoid infinite recursion when the config gets updated internally.
		// Reproduce issue by using "Find All References" to open the rift view and then right click again in the code outside of the view.
		delete (this._editor.getRawConfiguration() as IEditorConstructionOptions).model;

		(window as any).ed = this._editor; // for easier debugging via the JS console

		// Warm up the LSP server immediately when the document loads
		// instead of waiting until the user tries to hover.
		this._editor.onDidChangeModel((e: IModelChangedEvent) => {
			// only do it for modes we have called registerModeProviders on.
			if (!modes.has(this._editor.getModel().getModeId())) {
				return;
			}
			// We modify the name to indicate to our HTTP gateway that this
			// should not be measured as a user triggered action.
			lsp.send(this._editor.getModel(), "textDocument/hover?prepare", {
				textDocument: { uri: e.newModelUrl.toString(true) },
				position: lsp.toPosition(new Position(1, 1)),
			});
		});

		// Don't show context menu for peek view or comments, etc.
		// Also don't show for unsupported languages.
		this._editor.onContextMenu(e => {
			// HACK: This method relies on Monaco private internals.
			const isOnboarding = location.search.includes("ob=chrome");
			const peekWidget = e.target.detail === "vs.editor.contrib.zoneWidget1";
			const c = e.target.element.classList;
			const ignoreToken = c.contains("delimeter") || c.contains("comment") || c.contains("view-line") || (c.length === 1 && c.contains("token"));
			if (ignoreToken || peekWidget || this._editor.getModel() === initialModel || !isSupportedMode(this._editor.getModel().getModeId()) || isOnboarding) {
				(this._editor as any)._contextViewService.hideContextView();
				return;
			}

			// If we have a selection on right click, set it to the cursor
			// position. Otherwise, Monaco will use the selection end for eg
			// find all refs.
			if (!this._editor.getSelection().isEmpty() && e.target.position) {
				const range = {
					startLineNumber: e.target.position.lineNumber,
					startColumn: e.target.position.column,
					endLineNumber: e.target.position.lineNumber,
					endColumn: e.target.position.column,
				};
				this._editor.setSelection(range);
			}

			const {repo, rev, path} = URIUtils.repoParams(this._editor.getModel().uri);
			AnalyticsConstants.Events.CodeContextMenu_Initiated.logEvent({
				repo: repo,
				rev: rev || "",
				path: path,
				language: this._editor.getModel().getModeId(),
			}
			);

		});

		// Rename the "Find All References" action to "Find Local References".
		Object.assign((this._editor.getAction("editor.action.referenceSearch.trigger") || {}) as any, {
			_label: "Find Local References",
		});

		// Monaco overrides the back and forward history commands, so
		// we implement our own here. There currently isn't a way to
		// unbind a default keybinding.
		/* tslint:disable no-bitwise */
		this._editor.addCommand(KeyCode.LeftArrow | KeyMod.CtrlCmd, () => {
			global.window.history.back();
		}, "");
		this._editor.addCommand(KeyCode.RightArrow | KeyMod.CtrlCmd, () => {
			global.window.history.forward();
		}, "");
		/* tslint:enable no-bitwise */
		this._editor.addCommand(KeyCode.Home, () => {
			this._editor.revealLine(1);
		}, "");
		this._editor.addCommand(KeyCode.End, () => {
			this._editor.revealLine(
				this._editor.getModel().getLineCount()
			);
		}, "");

		let editorMenuItems = MenuRegistry.getMenuItems(MenuId.EditorContext);
		let commandOrder = {
			"editor.action.referenceSearch.trigger": 1.1,
			"editor.action.previewDeclaration": 1.2,
			"editor.action.goToDeclaration": 1.3,
		};
		for (let item of editorMenuItems) {
			item.order = commandOrder[item.command.id] || item.order;
			// HACK: VSCode doesn't have a clean API for removing context menu items
			// we don't want. The Copy action shows up always so remove it manually.
			if (item.command.id === "editor.action.clipboardCopyAction") {
				const idx = editorMenuItems.indexOf(item);
				if (idx >= 0) {
					editorMenuItems.splice(idx, 1);
				}
			}
		}

		// Set the dom readonly property, so keyboard doesn't pop up on mobile.
		const dom = this._editor.getDomNode();
		const input = dom.getElementsByClassName("inputarea");
		if (input.length === 1) {
			input[0].setAttribute("readOnly", "true");
		} else {
			console.error("Didn't set textarea to readOnly");
		}
	}

	onCursorSelectionChanged(listener: (e: ICursorSelectionChangedEvent) => void): void {
		this._editor.onDidChangeCursorSelection(listener);
	}

	setInput(uri: URI, range?: IRange): Promise<IEditor> {
		return new Promise<IEditor>((resolve, reject) => {
			this._editorService.openEditor({
				resource: uri,
				options: range ? { selection: range } : undefined,
			})
				.done(resolve, reject);
		});
	}

	public setSelection(range: IRange): void {
		this._editor.setSelection(range);
	}

	public getSelection(): any {
		return this._editor.getSelection();
	}

	public trigger(source: string, handlerId: string, payload: any): void {
		this._editor.trigger(source, handlerId, payload);
	}

	// An event emitted when the editor jumps to a new model or position therein.
	public onDidOpenEditor(listener: (e: IEditorOpenedEvent) => void): IDisposable {
		return this._editorService.onDidOpenEditor(listener);
	}

	toggleAuthors(visible: boolean): void {
		Features.codeLens.toggle();

		this._editor.updateOptions({ codeLens: visible });

		const {repo, rev, path} = URIUtils.repoParams(this._editor.getModel().uri);
		AnalyticsConstants.Events.AuthorsToggle_Clicked.logEvent({ visible, repo, rev, path });
	}

	public layout(): void {
		this._editor.layout();
	}

	public dispose(): void {
		this._editor.dispose();
		this._toDispose.forEach(disposable => {
			disposable.dispose();
		});
	}
}
