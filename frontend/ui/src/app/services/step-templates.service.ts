import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {ImportStepTemplateRequest, StepTemplate} from '../types/step-template';

const baseUrl = '/api/v1/step-templates';

@Injectable({
  providedIn: 'root',
})
export class StepTemplatesService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<StepTemplate[]> {
    return this.httpClient.get<StepTemplate[]>(baseUrl);
  }

  get(id: string): Observable<StepTemplate> {
    return this.httpClient.get<StepTemplate>(`${baseUrl}/${id}`);
  }

  importTemplate(request: ImportStepTemplateRequest): Observable<StepTemplate> {
    return this.httpClient.post<StepTemplate>(`${baseUrl}/import`, request);
  }
}
